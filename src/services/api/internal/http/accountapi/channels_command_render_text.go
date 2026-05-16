package accountapi

import (
	"fmt"
	"strings"
)

// RenderModelPickerText 生成纯文本渠道的 /models 完整输出。
// 按 provider 分组、编号列表，始终包含完整信息。
func RenderModelPickerText(data ModelPickerData) string {
	var sb strings.Builder
	sb.WriteString("可用模型：")
	idx := 1
	for _, pg := range data.Providers {
		sb.WriteString(fmt.Sprintf("\n\n[%s]", pg.Name))
		for _, m := range pg.Models {
			mark := ""
			if m.IsSelected {
				mark = " ✓"
			}
			sb.WriteString(fmt.Sprintf("\n%d. %s%s", idx, m.DisplayName, mark))
			idx++
		}
	}
	sb.WriteString(fmt.Sprintf("\n\n当前: %s", data.CurrentDisplay))
	sb.WriteString("\n切换: /model <编号或名称>")
	return sb.String()
}

// RenderModelStatusText 生成 /model 无参数时的状态显示。
func RenderModelStatusText(currentModel, thinkMode string) string {
	modelDisplay := currentModel
	if strings.TrimSpace(currentModel) == "" || currentModel == "跟随频道默认" {
		modelDisplay = "跟随频道默认"
	}
	thinkDisplay := thinkMode
	if thinkDisplay == "" {
		thinkDisplay = "off"
	}
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("当前模型: %s\n思考深度: %s", modelDisplay, thinkDisplay))
	sb.WriteString("\n\n切换模型: /models 查看列表\n切换思考: /think <level>")
	return sb.String()
}

// RenderThinkPickerText 生成 /think 的文本显示。
func RenderThinkPickerText(data ThinkPickerData) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("思考深度: %s\n", data.CurrentMode))
	for _, m := range data.Modes {
		mark := ""
		if m.IsSelected {
			mark = " ✓"
		}
		sb.WriteString(fmt.Sprintf("- %s%s\n", m.Name, mark))
	}
	sb.WriteString("切换: /think <level>")
	return sb.String()
}

// RenderPersonaPickerText 生成 /persona 的文本显示。
func RenderPersonaPickerText(data PersonaPickerData) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("当前 Persona: %s\n", data.CurrentName))
	for _, p := range data.Personas {
		mark := ""
		if p.IsSelected {
			mark = " ✓"
		}
		sb.WriteString(fmt.Sprintf("- %s%s\n", p.DisplayName, mark))
	}
	sb.WriteString("切换: /persona <名称>")
	return sb.String()
}

// RenderNewSessionText 生成 /new 的上下文感知确认。
func RenderNewSessionText(model, personaName string) string {
	var sb strings.Builder
	sb.WriteString("已开启新会话。")
	if model != "" || personaName != "" {
		sb.WriteString("\n")
		parts := make([]string, 0, 2)
		if model != "" {
			parts = append(parts, fmt.Sprintf("模型: %s", model))
		}
		if personaName != "" {
			parts = append(parts, fmt.Sprintf("Persona: %s", personaName))
		}
		sb.WriteString(strings.Join(parts, " | "))
	}
	sb.WriteString("\n直接发送消息开始对话。")
	return sb.String()
}

// RenderStatusText 生成 /status 的综合状态显示。
func RenderStatusText(model, thinkMode, personaName, runStatus string) string {
	modelDisplay := model
	if strings.TrimSpace(model) == "" {
		modelDisplay = "跟随频道默认"
	}
	thinkDisplay := thinkMode
	if thinkDisplay == "" {
		thinkDisplay = "off"
	}
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("模型: %s\n思考: %s", modelDisplay, thinkDisplay))
	if personaName != "" {
		sb.WriteString(fmt.Sprintf("\nPersona: %s", personaName))
	}
	sb.WriteString(fmt.Sprintf("\n状态: %s", runStatus))
	return sb.String()
}
