package coretest

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"

	"github.com/bini69-oi/worker/core"
)

// ErrPanelDown это ответ фейковой панели, когда она «лежит».
var ErrPanelDown = errors.New("панель недоступна")

// Panel это панель Remnawave в памяти. Абоненты ищутся по Ref, а без него по имени, как в настоящей.
type Panel struct {
	mu      sync.Mutex
	down    bool
	users   map[string]core.PanelUser // по Ref
	devices map[string][]core.PanelDevice
	links   map[string]int // номер выпуска ссылки
	next    int
	Calls   int
}

// NewPanel возвращает пустую живую панель.
func NewPanel() *Panel {
	return &Panel{users: map[string]core.PanelUser{}, devices: map[string][]core.PanelDevice{}, links: map[string]int{}}
}

// SetDown кладёт или поднимает панель.
func (p *Panel) SetDown(down bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.down = down
}

// User отдаёт абонента по имени, как его видит админ в панели.
func (p *Panel) User(username string) (core.PanelUser, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, u := range p.users {
		if u.Username == username {
			return u, true
		}
	}
	return core.PanelUser{}, false
}

// Edit правит абонента руками, как админ в панели.
func (p *Panel) Edit(username string, fn func(*core.PanelUser)) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for ref, u := range p.users {
		if u.Username == username {
			fn(&u)
			p.users[ref] = u
		}
	}
}

// AddDevice привязывает устройство абоненту.
func (p *Panel) AddDevice(ref, hwid string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.devices[ref] = append(p.devices[ref], core.PanelDevice{HWID: hwid})
}

func (p *Panel) result(ref string) core.PanelResult {
	u := p.users[ref]
	return core.PanelResult{Ref: ref, ExpireAt: u.ExpireAt, Enabled: u.Enabled,
		SubURL: fmt.Sprintf("https://sub.test/%s/%d", ref, p.links[ref])}
}

func (p *Panel) enter() error {
	p.Calls++
	if p.down {
		return ErrPanelDown
	}
	return nil
}

func (p *Panel) Sync(_ context.Context, u core.PanelUser) (core.PanelResult, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if err := p.enter(); err != nil {
		return core.PanelResult{}, err
	}
	ref := u.Ref
	if _, ok := p.users[ref]; !ok {
		ref = ""
		for r, old := range p.users {
			if old.Username == u.Username {
				ref = r
			}
		}
	}
	if ref == "" {
		p.next++
		ref = fmt.Sprintf("ref-%d", p.next)
	}
	u.Ref = ref
	u.Squads = slices.Clone(u.Squads)
	p.users[ref] = u
	return p.result(ref), nil
}

func (p *Panel) Get(_ context.Context, ref string) (core.PanelResult, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if err := p.enter(); err != nil {
		return core.PanelResult{}, err
	}
	if _, ok := p.users[ref]; !ok {
		return core.PanelResult{}, core.ErrNotFound
	}
	return p.result(ref), nil
}

func (p *Panel) Devices(_ context.Context, ref string) ([]core.PanelDevice, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if err := p.enter(); err != nil {
		return nil, err
	}
	return slices.Clone(p.devices[ref]), nil
}

func (p *Panel) DeleteDevice(_ context.Context, ref, hwid string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if err := p.enter(); err != nil {
		return err
	}
	p.devices[ref] = slices.DeleteFunc(p.devices[ref], func(d core.PanelDevice) bool { return d.HWID == hwid })
	return nil
}

func (p *Panel) ResetDevices(_ context.Context, ref string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if err := p.enter(); err != nil {
		return err
	}
	delete(p.devices, ref)
	return nil
}

func (p *Panel) RevokeLink(_ context.Context, ref string) (core.PanelResult, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if err := p.enter(); err != nil {
		return core.PanelResult{}, err
	}
	p.links[ref]++
	return p.result(ref), nil
}

// Notifier запоминает уведомления вместо отправки.
type Notifier struct {
	mu       sync.Mutex
	ToUsers  []core.NotifyTask
	ToAdmins []core.AdminTask
}

func (n *Notifier) User(_ context.Context, t core.NotifyTask) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.ToUsers = append(n.ToUsers, t)
	return nil
}

func (n *Notifier) Admin(_ context.Context, t core.AdminTask) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.ToAdmins = append(n.ToAdmins, t)
	return nil
}
