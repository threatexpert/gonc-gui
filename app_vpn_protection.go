package main

import (
	"context"
	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
	"gonc-gui/internal/goncrunner"
	"strings"
)

func confirmVPNBypassOverrides(ctx context.Context, plan goncrunner.ProtectionPlan, language string, show closeDialogFunc) (bool, error) {
	title, yes, no := "Confirm VPN bypass overrides", "Start VPN", "Cancel"
	local := plan.Local
	if local == "" {
		local = "Automatic physical source selection"
	}
	message := "Advanced arguments override the disconnect protection settings:\n" + strings.Join(plan.Overrides, "\n") + "\n\nEffective DNS: " + plan.DNS + "\nEffective local source: " + local + "\n\nAn incorrect source or DNS may prevent connection/reconnection. The DNS setting also affects other default DNS lookups in this app. Continue?"
	if language == "zh" {
		title, yes, no = "确认 VPN 旁路参数覆盖", "继续启用 VPN", "取消"
		if plan.Local == "" {
			local = "启动前自动选择物理出口"
		}
		message = "高级参数将覆盖断网保护自动准备的值：\n" + strings.Join(plan.Overrides, "\n") + "\n\n最终 DNS：" + plan.DNS + "\n最终本地源地址：" + local + "\n\n地址或 DNS 配置不正确可能导致连接、重连失败。DNS 设置也会影响本程序其他使用默认 DNS 的功能。是否继续？"
	}
	answer, err := show(ctx, wailsruntime.MessageDialogOptions{Type: wailsruntime.WarningDialog, Title: title, Message: message, Buttons: []string{yes, no}, DefaultButton: no, CancelButton: no})
	return err == nil && answer == yes, err
}
