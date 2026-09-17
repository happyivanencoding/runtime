package mcp

import "strings"

const (
	baseServerInstructions  = "优先调用 `runtime_context` 获取可用于操作用户设备的核心能力、Skill、动态 MCP 和重要上下文。处理多步骤任务时使用 `task_manage` 记录和维护任务进度。根据用户需求选择合适的能力检查、操作和验证设备状态。"
	nexusServerInstructions = "优先调用 `runtime_context` 获取可用于操作用户设备的核心能力、Skill、动态 MCP、Workflow 模板、重要上下文和长期记忆索引。需要查找或读取长期记忆时使用 `recall_*`；需要查找或使用 Workflow 模板时使用 `workflow_template_manage`；处理多步骤任务时使用 `task_manage` 记录和维护任务进度。根据用户需求选择合适的能力检查、操作和验证设备状态。"
)

func serverInstructions(nexusEnabled bool, custom string) string {
	instructions := baseServerInstructions
	if nexusEnabled {
		instructions = nexusServerInstructions
	}
	custom = strings.TrimSpace(custom)
	if custom == "" {
		return instructions
	}
	return instructions + "\n\nAdditional operator instructions:\n" + custom
}
