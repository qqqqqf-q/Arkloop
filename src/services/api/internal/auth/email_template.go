package auth

import (
	"fmt"
	"html"
	"strings"
)

// emailParams 控制邮件 HTML 的渲染内容。
type emailParams struct {
	Title     string // 卡片标题
	Greeting  string // 问候语，如 "Hi Alice,"
	BodyLines []string // 标题下方正文段落
	Code      string // 6 位验证码
	Notice    string // 验证码下方的小提示
	LinkURL   string // 可选 CTA 链接
	LinkLabel string // CTA 按钮文字
}

// buildEmailHTML 返回完整的 HTML 邮件字符串。
// 采用 light 主题（兼容所有主流邮件客户端），
// 风格与前端 light mode 保持一致。
func buildEmailHTML(p emailParams) string {
	var b strings.Builder

	// 颜色常量，与前端 light theme CSS 变量对应
	const (
		colorPageBg    = "#F5F5F4"
		colorCardBg    = "#FFFFFF"
		colorBorder    = "#E2E2E0"
		colorCodeBg    = "#F8F8F7"
		colorPrimary   = "#141412"
		colorSecondary = "#3D3D3B"
		colorMuted     = "#9C9A92"
		colorBtnBg     = "#1A1A18"
		colorBtnText   = "#FAFAFA"
		fontStack      = "-apple-system,BlinkMacSystemFont,'Segoe UI',Helvetica,Arial,sans-serif"
	)

	// -- head --
	b.WriteString(`<!DOCTYPE html><html lang="en"><head>`)
	b.WriteString(`<meta charset="UTF-8">`)
	b.WriteString(`<meta name="viewport" content="width=device-width,initial-scale=1.0">`)
	_, _ = fmt.Fprintf(&b, `<title>%s</title>`, html.EscapeString(p.Title))
	b.WriteString(`</head>`)

	// -- body --
	_, _ = fmt.Fprintf(&b,
		`<body style="margin:0;padding:0;background:%s;font-family:%s;-webkit-font-smoothing:antialiased;">`,
		colorPageBg, fontStack,
	)

	// 外层居中容器
	_, _ = fmt.Fprintf(&b,
		`<table width="100%%" cellpadding="0" cellspacing="0" role="presentation" style="background:%s;">`,
		colorPageBg,
	)
	b.WriteString(`<tr><td align="center" style="padding:52px 20px 40px;">`)

	// 品牌 wordmark
	_, _ = fmt.Fprintf(&b,
		`<p style="margin:0 0 28px;font-size:15px;font-weight:600;letter-spacing:-0.2px;color:%s;">Arkloop</p>`,
		colorPrimary,
	)

	// 卡片
	_, _ = fmt.Fprintf(&b,
		`<table width="100%%" cellpadding="0" cellspacing="0" role="presentation" `+
			`style="max-width:480px;background:%s;border:1px solid %s;border-radius:16px;`+
			`box-shadow:0 4px 24px rgba(0,0,0,0.07);">`,
		colorCardBg, colorBorder,
	)
	b.WriteString(`<tr><td style="padding:40px 40px 36px;">`)

	// 标题
	_, _ = fmt.Fprintf(&b,
		`<h1 style="margin:0 0 8px;font-size:20px;font-weight:600;letter-spacing:-0.3px;color:%s;">%s</h1>`,
		colorPrimary, html.EscapeString(p.Title),
	)

	// 问候语
	if p.Greeting != "" {
		_, _ = fmt.Fprintf(&b,
			`<p style="margin:0 0 24px;font-size:14px;line-height:1.65;color:%s;">%s</p>`,
			colorSecondary, html.EscapeString(p.Greeting),
		)
	}

	// 正文段落
	for _, line := range p.BodyLines {
		_, _ = fmt.Fprintf(&b,
			`<p style="margin:0 0 16px;font-size:14px;line-height:1.65;color:%s;">%s</p>`,
			colorSecondary, html.EscapeString(line),
		)
	}

	// 验证码区块
	if p.Code != "" {
		_, _ = fmt.Fprintf(&b,
			`<table width="100%%" cellpadding="0" cellspacing="0" role="presentation" `+
				`style="margin-bottom:16px;">`,
		)
		b.WriteString(`<tr><td align="center" style="` +
			fmt.Sprintf(`background:%s;border:1px solid %s;`, colorCodeBg, colorBorder) +
			`border-radius:12px;padding:28px 20px;">`)
		_, _ = fmt.Fprintf(&b,
			`<span style="font-size:36px;font-weight:700;letter-spacing:12px;color:%s;`+
				`font-variant-numeric:tabular-nums;display:inline-block;">%s</span>`,
			colorPrimary, html.EscapeString(p.Code),
		)
		b.WriteString(`</td></tr></table>`)
	}

	// 验证码下方提示
	if p.Notice != "" {
		_, _ = fmt.Fprintf(&b,
			`<p style="margin:0 0 28px;font-size:12px;color:%s;text-align:center;line-height:1.6;">%s</p>`,
			colorMuted, html.EscapeString(p.Notice),
		)
	}

	// CTA 按钮（可选）
	if p.LinkURL != "" && p.LinkLabel != "" {
		_, _ = fmt.Fprintf(&b,
			`<table width="100%%" cellpadding="0" cellspacing="0" role="presentation">`,
		)
		b.WriteString(`<tr><td align="center">`)
		_, _ = fmt.Fprintf(&b,
			`<a href="%s" target="_blank" style="`+
				`display:inline-block;padding:11px 28px;`+
				`background:%s;color:%s;`+
				`font-size:14px;font-weight:500;letter-spacing:-0.1px;`+
				`text-decoration:none;border-radius:10px;`+
				`font-family:%s;">%s</a>`,
			html.EscapeString(p.LinkURL), colorBtnBg, colorBtnText,
			fontStack, html.EscapeString(p.LinkLabel),
		)
		b.WriteString(`</td></tr></table>`)
	}

	// 卡片关闭
	b.WriteString(`</td></tr></table>`)

	// 分割线
	_, _ = fmt.Fprintf(&b,
		`<table width="100%%" cellpadding="0" cellspacing="0" role="presentation" style="max-width:480px;margin-top:0;">`,
	)
	b.WriteString(`<tr><td style="padding:0 40px;">`)
	_, _ = fmt.Fprintf(&b, `<hr style="border:none;border-top:1px solid %s;margin:0;">`, colorBorder)
	b.WriteString(`</td></tr></table>`)

	// 页脚
	_, _ = fmt.Fprintf(&b,
		`<p style="margin:20px 0 0;font-size:12px;color:%s;text-align:center;line-height:1.7;">`,
		colorMuted,
	)
	b.WriteString(`Arkloop &nbsp;·&nbsp; This email was sent automatically, please do not reply.</p>`)

	b.WriteString(`</td></tr></table>`)
	b.WriteString(`</body></html>`)

	return b.String()
}

// buildEmailHTMLZh 中文版本，内容由外部传入，结构与 buildEmailHTML 一致。
func buildEmailHTMLZh(p emailParams) string {
	// 结构完全相同，只改页脚文字
	original := buildEmailHTML(p)
	return strings.ReplaceAll(
		original,
		"This email was sent automatically, please do not reply.",
		"此邮件由系统自动发送，请勿直接回复。",
	)
}
