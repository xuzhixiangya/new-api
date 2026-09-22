package operation_setting

import (
	"strings"

	"github.com/QuantumNous/new-api/setting/config"
)

type SmartRouterSetting struct {
	Enabled             bool   `json:"enabled"`
	CheapModel          string `json:"cheap_model"`
	MidModel            string `json:"mid_model"`
	StrongModel         string `json:"strong_model"`
	ClassifierModel     string `json:"classifier_model"`
	ClassifierTimeoutMs int    `json:"classifier_timeout_ms"`
}

var smartRouterSetting = SmartRouterSetting{
	Enabled:             false,
	ClassifierTimeoutMs: 2000,
}

func init() {
	config.GlobalConfig.Register("smart_router_setting", &smartRouterSetting)
}

func GetSmartRouterSetting() *SmartRouterSetting {
	return &smartRouterSetting
}

func (s *SmartRouterSetting) Ready() bool {
	if s == nil || !s.Enabled {
		return false
	}
	return strings.TrimSpace(s.CheapModel) != "" &&
		strings.TrimSpace(s.MidModel) != "" &&
		strings.TrimSpace(s.StrongModel) != ""
}

func (s *SmartRouterSetting) ModelForTier(tier string) string {
	if s == nil {
		return ""
	}
	switch tier {
	case "cheap":
		return strings.TrimSpace(s.CheapModel)
	case "strong":
		return strings.TrimSpace(s.StrongModel)
	default:
		return strings.TrimSpace(s.MidModel)
	}
}

func (s *SmartRouterSetting) ClassifierModelName() string {
	if s == nil {
		return ""
	}
	if name := strings.TrimSpace(s.ClassifierModel); name != "" {
		return name
	}
	return strings.TrimSpace(s.CheapModel)
}

func (s *SmartRouterSetting) ClassifierTimeout() int {
	timeout := 2000
	if s != nil && s.ClassifierTimeoutMs > 0 {
		timeout = s.ClassifierTimeoutMs
	}
	return max(timeout, 2000)
}
