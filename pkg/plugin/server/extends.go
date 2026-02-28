package server

import (
	"context"
	"fmt"
	"strings"

	"github.com/fatedier/frp/pkg/util/util"
	"github.com/fatedier/frp/pkg/util/xlog"
)

type CloseClientContent struct {
	User UserInfo `json:"user"`
}

type ManagerExtends struct {
	closeClientPlugins []Plugin
}

const (
	OpCloseClient = "CloseClient" // 添加这一行
)

var managerExtends = &ManagerExtends{
	closeClientPlugins: make([]Plugin, 0),
}

func registerExtendsPlugin(m *Manager, p Plugin) {
	if p.IsSupport(OpCloseClient) {
		m.closeClientPlugins = append(m.closeClientPlugins, p)
	}
}

func (m *Manager) CloseClient(content *CloseClientContent) error {
	if len(m.closeClientPlugins) == 0 {
		return nil
	}

	errs := make([]string, 0)
	reqid, _ := util.RandID()
	xl := xlog.New().AppendPrefix("reqid: " + reqid)
	ctx := xlog.NewContext(context.Background(), xl)
	ctx = NewReqidContext(ctx, reqid)

	for _, p := range m.closeClientPlugins {
		_, _, err := p.Handle(ctx, OpCloseClient, *content)
		if err != nil {
			xl.Warnf("send CloseClient request to plugin [%s] error: %v", p.Name(), err)
			errs = append(errs, fmt.Sprintf("[%s]: %v", p.Name(), err))
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("send CloseClient request to plugin errors: %s", strings.Join(errs, "; "))
	}
	return nil
}
