package main

import (
	"context"
	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
	"gonc-gui/internal/goncrunner"
	"runtime"
	"strings"
)

func confirmVPNBypassOverrides(ctx context.Context, plan goncrunner.ProtectionPlan, language string, show closeDialogFunc) (bool, error) {
	title := "Confirm VPN bypass overrides"
	zh := language == "zh"
	// Wails uses native Yes/No responses on Windows and Linux.
	dialogType, yes, no := wailsruntime.QuestionDialog, "Yes", "No"
	if runtime.GOOS == "darwin" {
		dialogType, yes, no = wailsruntime.WarningDialog, "Start VPN", "Cancel"
		if zh {
			yes, no = "继续启用 VPN", "取消"
		}
	}
	local := plan.Local
	auto := "Automatic physical source selection"
	dnsNote := "The DNS setting also affects other default DNS lookups in this app."
	if zh {
		auto = "启动前自动选择物理出口"
		dnsNote = "DNS 设置也会影响本程序其他使用默认 DNS 的功能。"
	}
	if runtime.GOOS == "linux" {
		auto = "System routing"
		dnsNote = "This DNS setting applies only to the tunnel."
		if zh {
			auto = "系统路由"
			dnsNote = "此 DNS 仅供本次隧道使用。"
		}
	}
	if local == "" {
		local = auto
	}
	message := "Advanced arguments override the disconnect protection settings:\n" + strings.Join(plan.Overrides, "\n") + "\n\nEffective DNS: " + plan.DNS + "\nEffective local source: " + local + "\n\nAn incorrect source or DNS may prevent connection/reconnection. " + dnsNote + " Continue?"
	if zh {
		title = "确认 VPN 旁路参数覆盖"
		message = "高级参数将覆盖断网保护自动准备的值：\n" + strings.Join(plan.Overrides, "\n") + "\n\n最终 DNS：" + plan.DNS + "\n最终本地出口：" + local + "\n\n出口或 DNS 配置不正确可能导致连接、重连失败。" + dnsNote + "是否继续？"
	}
	answer, err := show(ctx, wailsruntime.MessageDialogOptions{Type: dialogType, Title: title, Message: message, Buttons: []string{yes, no}, DefaultButton: no, CancelButton: no})
	return err == nil && answer == yes, err
}
