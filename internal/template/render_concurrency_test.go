package template

import (
	"html/template"
	"sync"
	"testing"
)

func TestRenderDuringReload(t *testing.T) {
	funcs := func(name string) template.FuncMap {
		return template.FuncMap{"SiteName": func() string { return name }}
	}
	m := &Manager{funcMap: funcs("Support")}
	start := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		<-start
		for i := 0; i < 1000; i++ {
			if err := m.Reload(nil, nil, funcs("Helpdesk")); err != nil {
				t.Errorf("Reload: %v", err)
				return
			}
		}
	}()
	go func() {
		defer wg.Done()
		<-start
		for i := 0; i < 1000; i++ {
			got, err := m.Render(nil, `{{ SiteName }}`)
			if err != nil || (got != "Support" && got != "Helpdesk") {
				t.Errorf("Render = %q, %v; want a complete function map", got, err)
				return
			}
		}
	}()
	close(start)
	wg.Wait()
}
