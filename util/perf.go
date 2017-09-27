package util

import (
	"fmt"
	"github.com/Centny/gwf/log"
	"github.com/Centny/gwf/util"
	"github.com/shirou/gopsutil/cpu"
	"github.com/shirou/gopsutil/mem"
	"os"
	"sync"
	"sync/atomic"
	"time"
)

type Perf struct {
	stdout, stderr *os.File
	Max            int64         //最大耗时
	Min            int64         //最小耗时
	AveSuccess     float64       //成功的平均耗时
	AveAll         float64       //所有的平均耗时
	CostSuccess    int64         //成功的总耗时
	CostAll        int64         //总的耗时，不管成功与否
	PreCall        func() error  //环境预先检测
	SaveData       func() error  //保存过程数据
	Monitor        func()        //自定义监控方式
	TimeOut        int64         //自定义超时
	TimeOutCounts  int           //超时数量
	Running        int32         //正在运行的并发数
	RunningMax     int32         //最高并发数
	lck            *sync.RWMutex //读写加锁
	Done           int           //执行成功的次数
	Errc           int           //错误的次数
	MonitorRun     bool          //监控的当前状态
	MonitorWG      *sync.WaitGroup
	SaveDataRun    bool //开始收集测试数据
	SaveDataWG     *sync.WaitGroup
	DoPerfCost     int64
	OpenMonitor    bool
	OpenSaveData   bool
}

func NewPerf() *Perf {
	var p = &Perf{
		lck:        &sync.RWMutex{},
		MonitorWG:  &sync.WaitGroup{},
		SaveDataWG: &sync.WaitGroup{},
	}
	p.SetDefaultMonitor()
	p.SetDefaultSaveData()
	return p
}
func (p *Perf) SetMonitorStatue(statue bool) *Perf {
	p.OpenMonitor = statue
	return p
}

func (p *Perf) SetSaveDataStatue(statue bool) *Perf {
	p.OpenSaveData = statue
	return p
}

//设置默认的监控
func (p *Perf) SetDefaultMonitor() {
	p.Monitor = func() {
		vmem, _ := mem.VirtualMemory()
		vcpu, _ := cpu.Percent(100*time.Millisecond, false)
		fmt.Fprintf(p.stdout, "State cpu:%.1f%%,mem:%.1f%% \n",
			vcpu[0], vmem.UsedPercent)
		time.Sleep(time.Second)
	}
}

func (p *Perf) RunMonitor() {
	p.MonitorRun = true
	p.MonitorWG.Add(1)
	for p.MonitorRun {
		p.Monitor()
	}
	p.MonitorWG.Wait()
}

func (p *Perf) StartMonnitor() {
	if p.MonitorRun {
		return
	}
	go p.RunMonitor()
}

func (p *Perf) StopMonitor() {
	p.MonitorRun = false
	p.MonitorWG.Done()
}

//设置默认的数据保存方式
func (p *Perf) SetDefaultSaveData() {
	p.SaveData = func() error {
		fmt.Fprintf(p.stdout, "Running:%v, AveSuccess:%vms  TimeOutCounts:%v Done:%v Errc:%v\n",
			p.Running, p.AveSucc(), p.TimeOutCounts, p.Done, p.Errc)
		time.Sleep(time.Second)
		return nil
	}
}

func (p *Perf) RunSaveData() {
	p.SaveDataRun = true
	p.SaveDataWG.Add(1)
	for p.SaveDataRun {
		p.SaveData()
	}
	p.SaveDataWG.Wait()
}

func (p *Perf) StartSaveData() {
	if p.SaveDataRun {
		return
	}
	go p.RunSaveData()
}

func (p *Perf) StopSaveData() {
	p.SaveDataRun = false
	p.SaveDataWG.Done()
}

//在并发测试之前 用于校验服务器装填 网络情况等，用于取决是否适合运行测试
func (p *Perf) DoPreCall() error {
	return p.PreCall()
}

//测试总的耗时时长
func (p *Perf) RecordPerfCost(btime int64) {
	p.DoPerfCost = util.Now() - btime
}

//设置用例超时限制
func (p *Perf) SetTimeOut(t int64) *Perf {
	p.TimeOut = t
	return p
}

//单个用例执行结束之后的操作
func (p *Perf) perDone(cost int64, err error) {
	p.lck.Lock()
	defer p.lck.Unlock()
	if p.Running > p.RunningMax {
		p.RunningMax = p.Running
	}
	p.Running = p.Running - 1
	p.Done++
	p.CostAll = p.CostAll + cost
	if err == nil {
		if cost > p.Max {
			p.Max = cost
		}
		if cost > 0 && cost < p.Min {
			p.Min = cost
		}
		p.CostSuccess += cost
		if p.TimeOut > 0 && cost > p.TimeOut {
			p.TimeOutCounts++
		}
	} else {
		p.Errc++
	}
}

func (p *Perf) AveSucc() int {
	if p.Done-p.Errc == 0 {
		return 0
	}
	p.AveSuccess = float64(p.CostSuccess) / float64(p.Done-p.Errc)
	return int(p.AveSuccess)
}

func (p *Perf) AveA() int {
	if p.Done == 0 {
		return 0
	}
	p.AveAll = float64(p.CostSuccess) / float64(p.Done)
	return int(p.AveAll)
}

//并发测试方法
/*
@param
	total_count:总的运行次数
	init_count：初始运行次数
	per：当用例跑完之后的叠加量
	logf：是否打印到日志中，空则是打印到窗口，非空则会打印到指定的日志中
	call：测试用例，异步调用
*/
func (p *Perf) DoPerf(total_count int, init_count int, per int, logf string, call func(int) error) error {
	p.stdout, p.stderr = os.Stdout, os.Stderr
	if len(logf) > 0 {
		f, err := os.OpenFile(logf, os.O_APPEND|os.O_CREATE|os.O_WRONLY, os.ModePerm)
		if err != nil {
			return err
		}
		os.Stdout = f
		os.Stderr = f
		log.SetWriter(f)
	}
	defer func() {
		if len(logf) > 0 {
			os.Stdout.Close()
			os.Stdout = p.stdout
			os.Stderr = p.stderr
			log.SetWriter(os.Stdout)
		}
	}()
	//开启客户端状态监控
	if p.OpenMonitor {
		p.StartMonnitor()
		defer p.StopMonitor()
	}
	//保存运行过程中的数据 可用户生成折线图 或者测试报告
	if p.OpenSaveData {
		p.StartSaveData()
		defer p.StopSaveData()
	}
	//记录方法总的耗时时长
	defer p.RecordPerfCost(util.Now())

	ws := sync.WaitGroup{}
	var tidx_ int32 = 0
	var run_call, run_next func(int)
	run_call = func(v int) {
		perbeg := util.Now()
		terr := call(v)
		percost := util.Now() - perbeg
		p.perDone(percost, terr)
		//当running 数为0 的时候
		if p.Running <= 0 {
			run_next(v)
			//当既不报错 也 不超时的情况下
		} else if !(terr != nil || (percost > p.TimeOut && p.TimeOut > 0)) {
			run_next(v)
		}
		ws.Done()
	}
	var added = sync.RWMutex{}
	run_next = func(v int) {
		added.Lock()
		defer added.Unlock()
		for i := 0; i < per; i++ {
			ridx := int(atomic.AddInt32(&tidx_, 1))
			if ridx >= total_count {
				break
			}
			ws.Add(1)
			atomic.AddInt32(&p.Running, 1)
			go run_call(ridx)
		}
	}
	atomic.AddInt32(&tidx_, int32(init_count-1))
	for i := 0; i < init_count; i++ {
		ws.Add(1)
		atomic.AddInt32(&p.Running, 1)
		go run_call(i)
	}
	ws.Wait()
	return nil
}
