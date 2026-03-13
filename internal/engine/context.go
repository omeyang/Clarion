package engine

// DialogueContext 持有对话会话上下文，供规则评估使用。
// 放在 engine 包中以避免 dialogue 和 rules 之间的导入循环。
type DialogueContext struct {
	CurrentState      DialogueState
	Intent            Intent
	ExtractedFields   map[string]string
	RequiredFields    []string
	CollectedFields   map[string]string
	ObjectionCount    int
	MaxObjections     int
	TurnCount         int
	MaxTurns          int
	HighValue         bool
	NextActionDefined bool
}

// MissingFields 返回 RequiredFields 中尚未在 CollectedFields 中出现的字段。
func (c *DialogueContext) MissingFields() []string {
	var missing []string
	for _, f := range c.RequiredFields {
		if _, ok := c.CollectedFields[f]; !ok {
			missing = append(missing, f)
		}
	}
	return missing
}

// HasAllRequiredFields 当所有必填字段都已收集时返回 true。
func (c *DialogueContext) HasAllRequiredFields() bool {
	return len(c.MissingFields()) == 0
}
