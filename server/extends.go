package server

import plugin "github.com/fatedier/frp/pkg/plugin/server"

func extendControl(ctl *Control) {
	notifyContent := &plugin.CloseClientContent{
		User: plugin.UserInfo{
			User:  ctl.loginMsg.User,
			Metas: ctl.loginMsg.Metas,
			RunID: ctl.loginMsg.RunID,
		},
	}
	go func() {
		_ = ctl.pluginManager.CloseClient(notifyContent)
	}()
}
