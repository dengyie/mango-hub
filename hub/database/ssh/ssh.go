package ssh

import (
	"fmt"
	"strings"
	"time"

	"github.com/dengyie/mangohub/hub/database/dbcore"
	"github.com/dengyie/mangohub/hub/database/models"
	"github.com/dengyie/mangohub/hub/utils/crypto"
	"github.com/google/uuid"
)

// CreateSSHKey 创建并保存一个新的 SSH 密钥（私钥由 AES-256-GCM 加密存储）
func CreateSSHKey(name, scope, targetUUID, desc, pubKey, privKey string) (*models.SSHKey, error) {
	normPub, fp, err := models.ParseSSHPublicKey(pubKey)
	if err != nil {
		return nil, fmt.Errorf("invalid public key: %w", err)
	}

	encPriv := ""
	if strings.TrimSpace(privKey) != "" {
		enc, err := crypto.EncryptText(privKey)
		if err != nil {
			return nil, fmt.Errorf("encrypt private key: %w", err)
		}
		encPriv = enc
	}

	key := &models.SSHKey{
		ID:            uuid.New().String(),
		Name:          name,
		Algorithm:     "ed25519",
		PublicKey:     normPub,
		PrivateKeyEnc: encPriv,
		Fingerprint:   fp,
		Scope:         scope,
		TargetUUID:    targetUUID,
		Description:   desc,
		CreatedAt:     time.Now().UTC(),
		UpdatedAt:     time.Now().UTC(),
	}

	if err := dbcore.GetDBInstance().Create(key).Error; err != nil {
		return nil, err
	}
	return key, nil
}

// GenerateAndSaveSSHKey 自动生成一对 Ed25519 密钥对并入库
func GenerateAndSaveSSHKey(name, scope, targetUUID, desc string) (*models.SSHKey, string, error) {
	pubKey, privKey, _, err := models.GenerateEd25519Keypair(name)
	if err != nil {
		return nil, "", fmt.Errorf("generate keypair: %w", err)
	}

	key, err := CreateSSHKey(name, scope, targetUUID, desc, pubKey, privKey)
	if err != nil {
		return nil, "", err
	}
	return key, privKey, nil
}

// GetSSHKeyByID 获取单个 SSH 密钥
func GetSSHKeyByID(id string) (*models.SSHKey, error) {
	var key models.SSHKey
	if err := dbcore.GetDBInstance().Where("id = ?", id).First(&key).Error; err != nil {
		return nil, err
	}
	return &key, nil
}

// ListSSHKeys 列出所有管理的 SSH 密钥（私钥不返回）
func ListSSHKeys() ([]models.SSHKey, error) {
	var keys []models.SSHKey
	if err := dbcore.GetDBInstance().Order("created_at desc").Find(&keys).Error; err != nil {
		return nil, err
	}
	return keys, nil
}

// DeleteSSHKey 删除指定的 SSH 密钥
func DeleteSSHKey(id string) error {
	return dbcore.GetDBInstance().Where("id = ?", id).Delete(&models.SSHKey{}).Error
}

// CreateSSHTicket 创建互访申请 Ticket，自动生成专属于本次会话的单次临时 Ed25519 密钥对（Ephemeral Keypair）。
// 私钥 AES-256-GCM 加密存储于 Ticket 记录中，返回明文私钥供申请者使用。绝不暴露全局或静态共享私钥。
func CreateSSHTicket(sourceUUID, targetUUID, keyID, targetUser, targetHost string, targetPort int, requesterIP string, ttl time.Duration) (*models.SSHTicket, string, error) {
	if ttl <= 0 {
		ttl = 1 * time.Hour
	}

	ticketID := uuid.New().String()
	comment := fmt.Sprintf("mangohub-ticket-%s", ticketID)
	pubKey, privKey, _, err := models.GenerateEd25519Keypair(comment)
	if err != nil {
		return nil, "", fmt.Errorf("generate ephemeral keypair: %w", err)
	}

	encPriv, err := crypto.EncryptText(privKey)
	if err != nil {
		return nil, "", fmt.Errorf("encrypt ephemeral private key: %w", err)
	}

	ticket := &models.SSHTicket{
		TicketID:      ticketID,
		SourceClient:  sourceUUID,
		TargetClient:  targetUUID,
		SSHKeyID:      keyID,
		PublicKey:     pubKey,
		PrivateKeyEnc: encPriv,
		Status:        "active",
		TargetUser:    targetUser,
		TargetHost:    targetHost,
		TargetPort:    targetPort,
		RequesterIP:   requesterIP,
		ExpiresAt:     time.Now().UTC().Add(ttl),
		CreatedAt:     time.Now().UTC(),
	}
	if err := dbcore.GetDBInstance().Create(ticket).Error; err != nil {
		return nil, "", err
	}
	return ticket, privKey, nil
}

// GetSSHTicketByID 获取指定 Ticket
func GetSSHTicketByID(ticketID string) (*models.SSHTicket, error) {
	var ticket models.SSHTicket
	if err := dbcore.GetDBInstance().Where("ticket_id = ?", ticketID).First(&ticket).Error; err != nil {
		return nil, err
	}
	return &ticket, nil
}

// RevokeSSHTicket 吊销 Ticket（允许终结任何非 revoked 状态工单，具备幂等性）
func RevokeSSHTicket(ticketID string) error {
	now := time.Now().UTC()
	res := dbcore.GetDBInstance().Model(&models.SSHTicket{}).
		Where("ticket_id = ? AND status != ?", ticketID, "revoked").
		Updates(map[string]interface{}{
			"status":     "revoked",
			"revoked_at": &now,
		})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		var count int64
		if err := dbcore.GetDBInstance().Model(&models.SSHTicket{}).Where("ticket_id = ?", ticketID).Count(&count).Error; err != nil {
			return err
		}
		if count == 0 {
			return fmt.Errorf("ticket not found: %s", ticketID)
		}
	}
	return nil
}

// ExpireStaleTickets 将已过期的 Ticket 标记为 expired
func ExpireStaleTickets() error {
	now := time.Now().UTC()
	return dbcore.GetDBInstance().Model(&models.SSHTicket{}).
		Where("status = ? AND expires_at < ?", "active", now).
		Update("status", "expired").Error
}

// UpdateSSHTicketResult 更新 Ticket 执行状态与错误信息
// 终态保护：只有在非 revoked、expired 终态下才允许转为 failed，避免覆盖管理员主动吊销
func UpdateSSHTicketResult(ticketID string, success bool, errMsg string) error {
	if !success {
		return dbcore.GetDBInstance().Model(&models.SSHTicket{}).
			Where("ticket_id = ? AND status NOT IN ('revoked', 'expired')", ticketID).
			Updates(map[string]interface{}{
				"status":        "failed",
				"error_message": errMsg,
			}).Error
	}
	return dbcore.GetDBInstance().Model(&models.SSHTicket{}).
		Where("ticket_id = ?", ticketID).
		Update("error_message", "").Error
}

// TicketFilter 定义工单查询筛选条件
type TicketFilter struct {
	Status       string
	TargetClient string
	SourceClient string
	Limit        int
	Offset       int
}

// ListSSHTickets 分页查询 SSH 工单列表
func ListSSHTickets(filter TicketFilter) ([]models.SSHTicket, int64, error) {
	db := dbcore.GetDBInstance().Model(&models.SSHTicket{})
	if filter.Status != "" {
		db = db.Where("status = ?", filter.Status)
	}
	if filter.TargetClient != "" {
		db = db.Where("target_client = ?", filter.TargetClient)
	}
	if filter.SourceClient != "" {
		db = db.Where("source_client = ?", filter.SourceClient)
	}

	var total int64
	if err := db.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	limit := filter.Limit
	if limit <= 0 {
		limit = 50
	} else if limit > 200 {
		limit = 200
	}

	offset := filter.Offset
	if offset < 0 {
		offset = 0
	}

	var tickets []models.SSHTicket
	err := db.Order("created_at DESC").
		Limit(limit).
		Offset(offset).
		Find(&tickets).Error
	if err != nil {
		return nil, 0, err
	}
	return tickets, total, nil
}


