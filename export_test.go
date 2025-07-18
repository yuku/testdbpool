package testdbpool

import "github.com/yuku/numpool"

func (p *Pool) Config() *Config {
	return p.config
}

func (p *Pool) Manager() *numpool.Manager {
	return p.manager
}

func (p *Pool) Numpool() *numpool.Numpool {
	return p.numpool
}

func (p *Pool) TemplateDB() string {
	return p.templateDB
}

func (p *Pool) DatabaseNames() map[int]string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.databaseNames
}
