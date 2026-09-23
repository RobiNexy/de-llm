// list_rules.go / list_prompts.go — 设计文档 §4.6/§4.7 的子命令。
package command

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/RobiNexy/de-llm/pkg/config"
	"github.com/RobiNexy/de-llm/pkg/llm/prompt"
	"github.com/RobiNexy/de-llm/pkg/rule"
)

func newListRulesCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list-rules",
		Short: "列出所有可用的本地规则",
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			fmt.Fprintln(out, "\n本地规则:")
			for _, name := range rule.Names() {
				fmt.Fprintf(out, "  %-18s %s\n", name, rule.DescriptionOf(name))
			}
			return nil
		},
	}
}

func newListPromptsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list-prompts",
		Short: "列出所有已加载的提示词模板",
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()

			cfg, err := config.Load(config.Options{ConfigPath: flagConfig})
			if err != nil {
				return cfgErr("%v", err)
			}

			reg := prompt.NewRegistry()
			if err := reg.LoadBuiltin(prompt.Builtin); err != nil {
				return failErr("加载内置提示词失败: %v", err)
			}
			if err := reg.LoadDirs(cfg.LLM.Prompts.Dirs); err != nil {
				return failErr("加载外部提示词失败: %v", err)
			}

			fmt.Fprintln(out, "\n已加载的提示词模板:")
			for _, p := range reg.List() {
				fmt.Fprintf(out, "  %-28s %s  [%s]\n", p.Name, p.Description, p.Source)
			}
			return nil
		},
	}
}
