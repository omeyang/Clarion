package config

import (
	"os"
	"testing"
)

// FuzzConfigLoad 验证 Load 在任意 TOML 内容下不会 panic。
// 合法的 TOML 会被正常解析或触发验证错误，非法 TOML 返回解析错误。
func FuzzConfigLoad(f *testing.F) {
	f.Add([]byte(`
[server]
addr = ":9000"
`))
	f.Add([]byte(`invalid toml !!!`))
	f.Add([]byte(`
[server]
addr = ""
[database]
dsn = ""
`))
	f.Add([]byte(`
[database]
max_open_conns = -1
`))
	f.Add([]byte(``))

	f.Fuzz(func(t *testing.T, data []byte) {
		tmpFile, createErr := os.CreateTemp(t.TempDir(), "clarion-fuzz-*.toml")
		if createErr != nil {
			t.Skip("无法创建临时文件")
		}
		if _, writeErr := tmpFile.Write(data); writeErr != nil {
			tmpFile.Close()
			t.Skip("无法写入临时文件")
		}
		tmpFile.Close()

		// Load 不应 panic，只应返回 error 或有效 Config。
		cfg, err := Load(tmpFile.Name())
		if err != nil {
			return // 预期：无效配置返回错误。
		}

		// 如果解析成功，基本约束应满足。
		if cfg.Server.Addr == "" {
			t.Error("解析成功但 server.addr 为空")
		}
	})
}

// FuzzMaskDSN 验证 DSN 脱敏在任意输入下不会 panic 且不泄漏凭据。
func FuzzMaskDSN(f *testing.F) {
	f.Add("postgres://user:pass@localhost:5432/db")
	f.Add("")
	f.Add("no-at-sign")
	f.Add("@")
	f.Add("user@host")
	f.Add("://a:b@c")

	f.Fuzz(func(t *testing.T, dsn string) {
		result := maskDSN(dsn)

		// 结果必须以 "***" 开头，不泄漏 @ 之前的内容。
		if len(result) == 0 {
			t.Error("maskDSN 返回空字符串")
		}
		if result[0:3] != "***" {
			t.Errorf("maskDSN(%q) = %q，不以 *** 开头", dsn, result)
		}
	})
}
