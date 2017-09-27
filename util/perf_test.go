package util

import (
	"testing"
	"github.com/Centny/gwf/log"
	"time"
	"math/rand"
)

func TestNewPerf(t *testing.T) {
	p := NewPerf()
	p.PreCall = func() error{
		return nil
	}
	p.SetTimeOut(1000).SetSaveDataStatue(true)
	err := p.DoPreCall()
	if err != nil{
		t.Error(err)
		return
	}
	err = p.DoPerf(1000000,100,10,"", func(i int) error{
		var k = 0;
		k++
		time.Sleep(time.Millisecond * time.Duration(rand.New(rand.NewSource(time.Now().UnixNano())).Intn(2001)))
		return nil
	})
	if err != nil{
		t.Error(err)
		return
	}
	log.D("p :RunningMax-->%v  timeOutCount -->%v done -->%v DoPerfCost-->%v",p.RunningMax,p.TimeOutCounts,p.Done,p.DoPerfCost)
}
