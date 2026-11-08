package models

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
)

// SSHKey represents a managed SSH keypair in MangoHub
type SSHKey struct {
	ID            string    `json:"id" gorm:"type:varchar(36);primaryKey;unique"`
	Name          string    `json:"name" gorm:"type:varchar(100);not null"`
	Algorithm     string    `json:"algorithm" gorm:"type:varchar(50);default:'ed25519'"`
	PublicKey     string    `json:"public_key" gorm:"type:text;not null"`
	PrivateKeyEnc string    `json:"-" gorm:"type:text"` // AES-256-GCM encrypted private key
	Fingerprint   string    `json:"fingerprint" gorm:"type:varchar(100);index"`
	Scope         string    `json:"scope" gorm:"type:varchar(50);default:'global'"` // global, group, client
	TargetUUID    string    `json:"target_uuid,omitempty" gorm:"type:varchar(36);index"`
	Description   string    `json:"description,omitempty" gorm:"type:text"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

// SSHTicket represents a granted cross-node SSH access request
type SSHTicket struct {
	TicketID      string     `json:"ticket_id" gorm:"type:varchar(36);primaryKey;unique"`
	SourceClient  string     `json:"source_client" gorm:"type:varchar(36);index"`
	TargetClient  string     `json:"target_client" gorm:"type:varchar(36);index"`
	SSHKeyID      string     `json:"ssh_key_id" gorm:"type:varchar(36);index"`
	PublicKey     string     `json:"public_key" gorm:"type:text;not null"`
	PrivateKeyEnc string     `json:"-" gorm:"type:text"`                              // Ephemeral private key AES-256-GCM encrypted
	Status        string     `json:"status" gorm:"type:varchar(20);default:'active'"` // active, revoked, expired, failed
	ErrorMessage  string     `json:"error_message,omitempty" gorm:"type:text"`
	TargetUser    string     `json:"target_user" gorm:"type:varchar(50);default:'ubuntu'"`
	TargetHost    string     `json:"target_host" gorm:"type:varchar(100)"`
	TargetPort    int        `json:"target_port" gorm:"type:int;default:22"`
	RequesterIP   string     `json:"requester_ip" gorm:"type:varchar(100)"`
	ExpiresAt     time.Time  `json:"expires_at" gorm:"index"`
	RevokedAt     *time.Time `json:"revoked_at,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
}

// GenerateEd25519Keypair 生成一对标准的 OpenSSH Ed25519 公私钥对
func GenerateEd25519Keypair(comment string) (pubKeyStr, privKeyPEM, fingerprint string, err error) {
	pubKey, privKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return "", "", "", fmt.Errorf("generate ed25519: %w", err)
	}

	// 构造 SSH 公钥格式
	sshPubKey, err := ssh.NewPublicKey(pubKey)
	if err != nil {
		return "", "", "", fmt.Errorf("convert ssh public key: %w", err)
	}
	pubKeyBytes := ssh.MarshalAuthorizedKey(sshPubKey)
	pubKeyStr = strings.TrimSpace(string(pubKeyBytes))
	if comment != "" {
		pubKeyStr += " " + comment
	}

	// 计算指纹 SHA256:...
	sha := sha256.Sum256(sshPubKey.Marshal())
	fingerprint = "SHA256:" + strings.TrimRight(base64.StdEncoding.EncodeToString(sha[:]), "=")

	// OpenSSH 私钥封装
	privBlock, err := ssh.MarshalPrivateKey(privKey, comment)
	if err != nil {
		return "", "", "", fmt.Errorf("marshal private key: %w", err)
	}
	privKeyPEM = string(pem.EncodeToMemory(privBlock))

	return pubKeyStr, privKeyPEM, fingerprint, nil
}

// ParseSSHPublicKey 解析公钥并返回标准化格式和指纹
func ParseSSHPublicKey(pubKeyStr string) (normalized string, fingerprint string, err error) {
	pubKeyBytes := []byte(strings.TrimSpace(pubKeyStr))
	pubKey, comment, _, _, err := ssh.ParseAuthorizedKey(pubKeyBytes)
	if err != nil {
		return "", "", fmt.Errorf("parse authorized key: %w", err)
	}
	normalized = strings.TrimSpace(string(ssh.MarshalAuthorizedKey(pubKey)))
	if comment != "" {
		normalized += " " + comment
	}
	sha := sha256.Sum256(pubKey.Marshal())
	fingerprint = "SHA256:" + strings.TrimRight(base64.StdEncoding.EncodeToString(sha[:]), "=")
	return normalized, fingerprint, nil
}
