package main

import (
	"github.com/bini69-oi/worker/pay"
	"github.com/bini69-oi/worker/pay/antilopay"
	"github.com/bini69-oi/worker/pay/aurapay"
	"github.com/bini69-oi/worker/pay/cispay"
	"github.com/bini69-oi/worker/pay/cloudpayments"
	"github.com/bini69-oi/worker/pay/cryptobot"
	"github.com/bini69-oi/worker/pay/cryptomus"
	"github.com/bini69-oi/worker/pay/donut"
	"github.com/bini69-oi/worker/pay/etoplatezhi"
	"github.com/bini69-oi/worker/pay/freekassa"
	"github.com/bini69-oi/worker/pay/heleket"
	"github.com/bini69-oi/worker/pay/jupiter"
	"github.com/bini69-oi/worker/pay/kassaai"
	"github.com/bini69-oi/worker/pay/lava"
	"github.com/bini69-oi/worker/pay/manual"
	"github.com/bini69-oi/worker/pay/mulenpay"
	"github.com/bini69-oi/worker/pay/overpay"
	"github.com/bini69-oi/worker/pay/paritypay"
	"github.com/bini69-oi/worker/pay/paypalych"
	"github.com/bini69-oi/worker/pay/paypear"
	"github.com/bini69-oi/worker/pay/platega"
	"github.com/bini69-oi/worker/pay/riopay"
	"github.com/bini69-oi/worker/pay/robokassa"
	"github.com/bini69-oi/worker/pay/rollypay"
	"github.com/bini69-oi/worker/pay/severpay"
	"github.com/bini69-oi/worker/pay/stars"
	"github.com/bini69-oi/worker/pay/stripe"
	"github.com/bini69-oi/worker/pay/tabpay"
	"github.com/bini69-oi/worker/pay/tbank"
	"github.com/bini69-oi/worker/pay/tribute"
	"github.com/bini69-oi/worker/pay/wata"
	"github.com/bini69-oi/worker/pay/yookassa"
	"github.com/bini69-oi/worker/pay/yoomoney"
	"github.com/bini69-oi/worker/settings"
)

// gateways это все кассы. Новая касса добавляется сюда одной строкой. t даёт кассам адрес возврата в бота
// и возврат звёзд через Telegram.
func gateways(cfg *settings.Settings, t *telegram, publicURL string) []pay.Gateway {
	kassa := yookassa.New(cfg, "", nil)
	kassa.RestrictIPs(yookassa.OfficialNets()) // уведомления ЮKassa только с её адресов
	kassa.Return = t.paidURL
	pl := platega.New(cfg, "", nil)
	pl.Return = t.paidURL
	st := stars.New(cfg)
	st.Refunder = t.client.RefundStarPayment
	hel := heleket.New(cfg, "", nil)
	hel.Return, hel.Hook = t.paidURL, publicURL+"/pay/heleket"
	cm := cryptomus.New(cfg, "", nil)
	cm.Return, cm.Hook = t.paidURL, publicURL+"/pay/cryptomus"
	ym := yoomoney.New(cfg, "", nil)
	ym.Return = t.paidURL
	fk := freekassa.New(cfg, "", nil)
	fk.RestrictIPs(freekassa.OfficialNets()) // уведомления FreeKassa только с её адресов
	wt := wata.New(cfg, "", nil)
	wt.Return = t.paidURL
	lv := lava.New(cfg, "", nil)
	lv.Return, lv.Hook = t.paidURL, publicURL+"/pay/lava"
	cp := cloudpayments.New(cfg, "", nil)
	cp.Return = t.paidURL
	pp := paypalych.New(cfg, "", nil)
	pp.Return = t.paidURL
	mp := mulenpay.New(cfg, "", nil)
	mp.Return = t.paidURL
	ap := antilopay.New(cfg, "", nil)
	ap.Return = t.paidURL
	sv := severpay.New(cfg, "", nil)
	sv.Return = t.paidURL
	rp := rollypay.New(cfg, "", nil)
	rp.Return = t.paidURL
	ar := aurapay.New(cfg, "", nil)
	ar.Return, ar.Hook = t.paidURL, publicURL+"/pay/aurapay"
	cs := cispay.New(cfg, "", nil)
	cs.Return = t.paidURL
	ka := kassaai.New(cfg, "", nil)
	ka.Return, ka.Hook = t.paidURL, publicURL+"/pay/kassaai"
	ri := riopay.New(cfg, "", nil)
	ri.Return = t.paidURL
	pr := paypear.New(cfg, "", nil)
	pr.Return, pr.Hook = t.paidURL, publicURL+"/pay/paypear"
	ov := overpay.New(cfg, "", nil)
	ov.Return = t.paidURL
	ep := etoplatezhi.New(cfg, "", nil)
	ep.Return, ep.Hook = t.paidURL, publicURL+"/pay/etoplatezhi"
	ju := jupiter.New(cfg, "", nil)
	ju.Hook = publicURL + "/pay/jupiter"
	tb := tabpay.New(cfg, "", nil)
	tb.Return = t.paidURL
	py := paritypay.New(cfg, "", nil)
	py.Return, py.Hook = t.paidURL, publicURL+"/pay/paritypay"
	dn := donut.New(cfg, "", nil)
	dn.Return, dn.Hook = t.paidURL, publicURL+"/pay/donut"
	tk := tbank.New(cfg, "", nil)
	tk.Return, tk.Hook = t.paidURL, publicURL+"/pay/tbank"
	sr := stripe.New(cfg, "", nil)
	sr.Return = t.paidURL
	return []pay.Gateway{
		manual.New(cfg),
		st,
		kassa,
		cryptobot.New(cfg, "", nil),
		pl,
		hel,
		cm,
		ym,
		fk,
		wt,
		robokassa.New(cfg, "", nil),
		lv,
		cp,
		pp,
		tribute.New(cfg), // поиск платежа по донату ставит build: ему нужно ядро
		mp,
		ap,
		sv,
		rp,
		ar,
		cs,
		ka,
		ri,
		pr,
		ov,
		ep,
		ju,
		tb,
		py,
		dn,
		tk,
		sr,
	}
}
