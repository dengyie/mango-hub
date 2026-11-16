package api

import (
	"net/http"
	"strings"
	"time"

	"github.com/dengyie/mangohub/hub/database/accounts"
	"github.com/dengyie/mangohub/hub/database/clients"
	agent_runtime "github.com/dengyie/mangohub/hub/web/agent"
	v2 "github.com/dengyie/mangohub/protocol/v2"
	"github.com/gin-gonic/gin"
)

// publicWSReadLimit bounds one WebSocket frame on visitor-facing endpoints:
// gorilla buffers the whole frame before any handler runs, so without a limit
// a single oversized text frame on an anonymous connection is a remote OOM.
const publicWSReadLimit = 1 << 20 // 1 MiB

// publicWSIdleTimeout evicts visitor WebSocket connections that send nothing:
// the loops below read with no deadline otherwise, letting dead clients pin
// a goroutine and a connection forever (slowloris-style slot retention).
const publicWSIdleTimeout = 5 * time.Minute

func GetClients(c *gin.Context) {
	// 升级到ws
	if !IsWebSocketUpgrade(c) {
		c.JSON(http.StatusBadRequest, gin.H{"status": "error", "error": "Require WebSocket upgrade"})
		return
	}
	// Upgrade the HTTP connection to a WebSocket connection
	conn, err := UpgradeSafeConn(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"status": "error", "error": "Failed to upgrade to WebSocket." + err.Error()})
		return
	}
	defer conn.Close()
	conn.SetReadLimit(publicWSReadLimit)

	// 初始化用户信息
	var (
		isLogin    = false
		session, _ = c.Cookie("session_token")
	)

	// 登录状态检查
	_, err = accounts.GetUserBySession(session)
	if err == nil {
		isLogin = true
	}

	// 请求
	for {
		// Idle deadline: refreshed on every request the viewer sends.
		_ = conn.SetReadDeadline(time.Now().Add(publicWSIdleTimeout))
		var resp struct {
			Online []string             `json:"online"` // 已建立连接的客户端uuid列表
			Data   map[string]v2.Report `json:"data"`   // 最后上报的数据
		}

		resp.Online = []string{}
		resp.Data = map[string]v2.Report{}

		_, data, err := conn.ReadMessage()
		if err != nil {
			return
		}
		message := string(data)

		uuID := ""
		if message != "get" { // 非请求全部内容
			if strings.HasPrefix(message, "get ") {
				uuID = strings.TrimSpace(strings.TrimPrefix(message, "get "))
			} else {
				conn.WriteJSON(gin.H{"status": "error", "error": "Invalid message"})
				continue
			}
		}

		// 在线客户端uuid列表（WebSocket 与非 WebSocket）
		for _, key := range agent_runtime.GetAllOnlineUUIDs() {
			if !isLogin && clients.IsHiddenClient(key) {
				continue
			}
			if uuID != "" && key != uuID {
				continue
			}
			resp.Online = append(resp.Online, key)
		}

		//过往节点数据信息
		for key, report := range agent_runtime.GetLatestReport() {
			if !isLogin && clients.IsHiddenClient(key) {
				continue
			}
			if uuID != "" && key != uuID {
				continue
			}

			report.UUID = "" // 不暴露 uuid
			if !isLogin {
				redactReportForGuest(report)
			}
			if report.CPU.Usage == 0 {
				report.CPU.Usage = 0.01
			}
			resp.Data[key] = *report
		}

		err = conn.WriteJSON(gin.H{"status": "success", "data": resp})
		if err != nil {
			return
		}
	}
}

// redactReportForGuest enforces the guest-visibility policy on the live
// report stream, matching what publicGetClientRecentRecords and
// getNodesLatestStatus already do: mining wallet/pool addresses, GPU
// serial-level details and disk IO are admin/owner data, not public homepage
// material. The homepage only renders CPU/RAM/load/net numbers.
func redactReportForGuest(report *v2.Report) {
	report.Mining = nil
	report.GPU = nil
	report.DiskIO = nil
	report.Message = ""
}
