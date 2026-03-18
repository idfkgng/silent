package main

import "sync/atomic"

// Stats holds all checker counters, safe for concurrent use.
type Stats struct {
	Hits          int64
	Bad           int64
	SFA           int64
	MFA           int64
	TwoFA         int64
	XGP           int64
	XGPU          int64
	Other         int64
	ValidMail     int64
	Retries       int64
	Errors        int64
	Checked       int64
	DonutBanned   int64
	DonutUnbanned int64
	Total         int64
}

var gStats = &Stats{}

func (s *Stats) Reset(total int64) {
	atomic.StoreInt64(&s.Hits, 0)
	atomic.StoreInt64(&s.Bad, 0)
	atomic.StoreInt64(&s.SFA, 0)
	atomic.StoreInt64(&s.MFA, 0)
	atomic.StoreInt64(&s.TwoFA, 0)
	atomic.StoreInt64(&s.XGP, 0)
	atomic.StoreInt64(&s.XGPU, 0)
	atomic.StoreInt64(&s.Other, 0)
	atomic.StoreInt64(&s.ValidMail, 0)
	atomic.StoreInt64(&s.Retries, 0)
	atomic.StoreInt64(&s.Errors, 0)
	atomic.StoreInt64(&s.Checked, 0)
	atomic.StoreInt64(&s.DonutBanned, 0)
	atomic.StoreInt64(&s.DonutUnbanned, 0)
	atomic.StoreInt64(&s.Total, total)
}

func (s *Stats) Snapshot() Stats {
	return Stats{
		Hits:          atomic.LoadInt64(&s.Hits),
		Bad:           atomic.LoadInt64(&s.Bad),
		SFA:           atomic.LoadInt64(&s.SFA),
		MFA:           atomic.LoadInt64(&s.MFA),
		TwoFA:         atomic.LoadInt64(&s.TwoFA),
		XGP:           atomic.LoadInt64(&s.XGP),
		XGPU:          atomic.LoadInt64(&s.XGPU),
		Other:         atomic.LoadInt64(&s.Other),
		ValidMail:     atomic.LoadInt64(&s.ValidMail),
		Retries:       atomic.LoadInt64(&s.Retries),
		Errors:        atomic.LoadInt64(&s.Errors),
		Checked:       atomic.LoadInt64(&s.Checked),
		DonutBanned:   atomic.LoadInt64(&s.DonutBanned),
		DonutUnbanned: atomic.LoadInt64(&s.DonutUnbanned),
		Total:         atomic.LoadInt64(&s.Total),
	}
}

func incHit()     { atomic.AddInt64(&gStats.Hits, 1); atomic.AddInt64(&gStats.Checked, 1) }
func incBad()     { atomic.AddInt64(&gStats.Bad, 1); atomic.AddInt64(&gStats.Checked, 1) }
func incSFA()     { atomic.AddInt64(&gStats.SFA, 1); atomic.AddInt64(&gStats.Checked, 1) }
func incMFA()     { atomic.AddInt64(&gStats.MFA, 1) }
func inc2FA()     { atomic.AddInt64(&gStats.TwoFA, 1); atomic.AddInt64(&gStats.Checked, 1) }
func incXGP()     { atomic.AddInt64(&gStats.XGP, 1); atomic.AddInt64(&gStats.Checked, 1) }
func incXGPU()    { atomic.AddInt64(&gStats.XGPU, 1); atomic.AddInt64(&gStats.Checked, 1) }
func incOther()   { atomic.AddInt64(&gStats.Other, 1); atomic.AddInt64(&gStats.Checked, 1) }
func incVM()      { atomic.AddInt64(&gStats.ValidMail, 1); atomic.AddInt64(&gStats.Checked, 1) }
func incRetry()   { atomic.AddInt64(&gStats.Retries, 1) }
func incError()   { atomic.AddInt64(&gStats.Errors, 1) }
func incDonutB()  { atomic.AddInt64(&gStats.DonutBanned, 1) }
func incDonutUB() { atomic.AddInt64(&gStats.DonutUnbanned, 1) }
