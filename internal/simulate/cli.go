// Package simulate 提供基于文本的 CLI 模拟器，用于测试对话流程，
// 无需 FreeSWITCH 或真实电话基础设施。创建对话引擎并从 stdin 读取
// 用户输入，输出机器人回复和调试信息（状态转换、意图、收集的字段）。
package simulate

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log/slog"
	"sort"
	"strings"

	"github.com/omeyang/clarion/internal/engine"
	"github.com/omeyang/clarion/internal/engine/dialogue"
	"github.com/omeyang/clarion/internal/engine/rules"
	"github.com/omeyang/clarion/internal/provider"
)

// SimulatorConfig 配置文本模拟器。
type SimulatorConfig struct {
	LLM             provider.LLMProvider
	Logger          *slog.Logger
	PromptTemplates map[string]string
	SystemPrompt    string
	RequiredFields  []string
	MaxTurns        int
	Input           io.Reader
	Output          io.Writer
}

// Simulator 从终端驱动交互式对话会话。
type Simulator struct {
	engine *dialogue.Engine
	cfg    SimulatorConfig
	logger *slog.Logger
	input  io.Reader
	output io.Writer
}

// NewSimulator 创建 Simulator。使用提供的配置初始化对话引擎。
func NewSimulator(cfg SimulatorConfig) (*Simulator, error) {
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}

	maxTurns := cfg.MaxTurns
	if maxTurns <= 0 {
		maxTurns = 20
	}

	templateConfig := rules.TemplateConfig{
		RequiredFields: cfg.RequiredFields,
		MaxObjections:  3,
		MaxTurns:       maxTurns,
		Templates: map[string]string{
			"OPENING": "您好，打扰您一分钟。",
			"CLOSING": "感谢您的时间，再见！",
		},
	}

	if cfg.PromptTemplates == nil {
		cfg.PromptTemplates = map[string]string{
			"OPENING": "您好，打扰您一分钟。",
		}
	}

	eng, err := dialogue.NewEngine(dialogue.EngineConfig{
		TemplateConfig:  templateConfig,
		LLM:             cfg.LLM,
		Logger:          logger,
		PromptTemplates: cfg.PromptTemplates,
		SystemPrompt:    cfg.SystemPrompt,
		MaxHistory:      5,
	})
	if err != nil {
		return nil, fmt.Errorf("create dialogue engine: %w", err)
	}

	input := cfg.Input
	if input == nil {
		input = strings.NewReader("")
	}

	output := cfg.Output
	if output == nil {
		output = io.Discard
	}

	return &Simulator{
		engine: eng,
		cfg:    cfg,
		logger: logger,
		input:  input,
		output: output,
	}, nil
}

// Run 启动交互式模拟循环。阻塞直到对话结束、ctx 取消或输入到达 EOF。
func (s *Simulator) Run(ctx context.Context) error {
	s.printHeader()

	// 输出开场白。
	opening := s.engine.GetOpeningText()
	s.printBotReply(opening)
	s.printState()

	scanner := bufio.NewScanner(s.input)
	for {
		select {
		case <-ctx.Done():
			s.printf("\n[系统] 模拟已取消\n")
			return nil
		default:
		}

		if s.engine.IsFinished() {
			s.printResult()
			return nil
		}

		s.printf("\n你: ")
		if !scanner.Scan() {
			// EOF 或扫描器错误。
			s.printf("\n[系统] 输入结束\n")
			s.printResult()
			return nil
		}

		userText := strings.TrimSpace(scanner.Text())
		if userText == "" {
			continue
		}

		if userText == "/quit" || userText == "/exit" {
			s.printf("[系统] 退出模拟\n")
			s.printResult()
			return nil
		}

		if userText == "/state" {
			s.printState()
			continue
		}

		reply, err := s.engine.ProcessUserInput(ctx, userText)
		if err != nil {
			s.printf("[错误] %v\n", err)
			continue
		}

		s.printBotReply(reply)
		s.printState()
	}
}

func (s *Simulator) printHeader() {
	s.printf("╔══════════════════════════════════════╗\n")
	s.printf("║       Clarion 对话模拟器              ║\n")
	s.printf("╠══════════════════════════════════════╣\n")
	s.printf("║ 输入文字模拟用户说话                   ║\n")
	s.printf("║ /quit 退出  /state 查看状态            ║\n")
	s.printf("╚══════════════════════════════════════╝\n\n")
}

func (s *Simulator) printBotReply(reply string) {
	s.printf("机器人: %s\n", reply)
}

func (s *Simulator) printState() {
	state := s.engine.State()
	turns := s.engine.Turns()
	result := s.engine.Result(engine.CallInProgress)

	s.printf("  [状态] %s", state.String())

	if len(result.CollectedFields) > 0 {
		keys := make([]string, 0, len(result.CollectedFields))
		for k := range result.CollectedFields {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		var pairs []string
		for _, k := range keys {
			pairs = append(pairs, fmt.Sprintf("%s=%s", k, result.CollectedFields[k]))
		}
		s.printf(" | 字段: {%s}", strings.Join(pairs, ", "))
	}

	if len(result.MissingFields) > 0 {
		s.printf(" | 缺失: [%s]", strings.Join(result.MissingFields, ", "))
	}

	s.printf(" | 轮次: %d", len(turns))
	s.printf("\n")
}

func (s *Simulator) printResult() {
	result := s.engine.Result(engine.CallCompleted)

	s.printf("\n╔══════════════════════════════════════╗\n")
	s.printf("║             对话结果                  ║\n")
	s.printf("╠══════════════════════════════════════╣\n")
	s.printf("║ 评级: %-32s║\n", string(result.Grade))
	s.printf("║ 轮次: %-32d║\n", result.TurnCount)

	if len(result.CollectedFields) > 0 {
		s.printf("║ 收集字段:                            ║\n")
		keys := make([]string, 0, len(result.CollectedFields))
		for k := range result.CollectedFields {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			s.printf("║   %-36s║\n", fmt.Sprintf("%s: %s", k, result.CollectedFields[k]))
		}
	}

	if len(result.MissingFields) > 0 {
		s.printf("║ 缺失字段: %-28s║\n", strings.Join(result.MissingFields, ", "))
	}

	s.printf("╚══════════════════════════════════════╝\n")
}

func (s *Simulator) printf(format string, args ...any) {
	_, _ = fmt.Fprintf(s.output, format, args...)
}
