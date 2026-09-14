package model

// TLS 指纹白名单唯一权威（C250913-08）：op/channel.go 与 model/site.go 此前
// 各有一份逐字相同的 switch，client 另有一套同名常量——加第三种指纹时是漏改点。
const (
	TLSFingerprintChrome  = "chrome"
	TLSFingerprintFirefox = "firefox"
)

// NormalizeTLSFingerprint 返回白名单内的规范值，其余归空串。
func NormalizeTLSFingerprint(value string) string {
	switch value {
	case TLSFingerprintChrome, TLSFingerprintFirefox:
		return value
	default:
		return ""
	}
}
