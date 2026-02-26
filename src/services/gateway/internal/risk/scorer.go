package risk

import (
	"net/http"

	"arkloop/services/gateway/internal/geoip"
	"arkloop/services/gateway/internal/ua"
)

// Scorer 根据请求信号计算风险分。
type Scorer struct {
	// RejectThreshold 超过此分值时 Scorer.ShouldReject 返回 true。
	// 0 表示禁用拒绝（只记录，不阻断）。
	RejectThreshold int
}

// Evaluate 计算请求的风险分。
func (s *Scorer) Evaluate(r *http.Request, geo geoip.Result, uaInfo ua.Info, anonymous bool) Score {
	var score int
	var signals []string

	// 无身份认证
	if anonymous {
		score += 10
		signals = append(signals, "no_auth")
	}

	// Tor 出口节点或匿名 VPN
	if geo.Type == geoip.IPTypeTor {
		score += 60
		signals = append(signals, "tor_exit_node")
	}

	// 数据中心 / 云主机 IP（非住宅 IP）
	if geo.Type == geoip.IPTypeHosting {
		score += 30
		signals = append(signals, "datacenter_ip")
	}

	// 空 User-Agent
	if uaInfo.Type == ua.TypeUnknown {
		score += 20
		signals = append(signals, "empty_ua")
	}

	// 已知爬虫 / 扫描器
	if uaInfo.Type == ua.TypeBot {
		score += 15
		signals = append(signals, "bot_ua")
	}

	if score > 100 {
		score = 100
	}

	return Score{
		Value:   score,
		Level:   scoreToLevel(score),
		Signals: signals,
	}
}

// ShouldReject 返回 true 表示 Gateway 应直接拒绝请求。
func (s *Scorer) ShouldReject(score Score) bool {
	return s.RejectThreshold > 0 && score.Value >= s.RejectThreshold
}
