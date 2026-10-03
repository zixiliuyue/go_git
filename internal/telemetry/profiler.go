package telemetry

import (
	"fmt"
	"os"
	"runtime"
	"runtime/pprof"
	"runtime/trace"
)

// Profiler 统一管理 CPU Profile、Memory Heap Profile 与 Execution Trace
type Profiler struct {
	cpuFile     *os.File
	memFilePath string
	traceFile   *os.File
}

// StartProfiler 根据传入的分析文件路径启动分析采集器
func StartProfiler(cpuProfile, memProfile, traceProfile string) (*Profiler, error) {
	if cpuProfile == "" && memProfile == "" && traceProfile == "" {
		return nil, nil
	}

	p := &Profiler{
		memFilePath: memProfile,
	}

	// 1. 启动 CPU Profiler
	if cpuProfile != "" {
		f, err := os.Create(cpuProfile)
		if err != nil {
			return nil, fmt.Errorf("创建 cpuprofile 文件失败 (%s): %w", cpuProfile, err)
		}
		if err := pprof.StartCPUProfile(f); err != nil {
			_ = f.Close()
			return nil, fmt.Errorf("启动 CPU profile 失败: %w", err)
		}
		p.cpuFile = f
	}

	// 2. 启动 Execution Tracer
	if traceProfile != "" {
		f, err := os.Create(traceProfile)
		if err != nil {
			p.cleanPartial()
			return nil, fmt.Errorf("创建 trace 文件失败 (%s): %w", traceProfile, err)
		}
		if err := trace.Start(f); err != nil {
			_ = f.Close()
			p.cleanPartial()
			return nil, fmt.Errorf("启动 Execution Trace 失败: %w", err)
		}
		p.traceFile = f
	}

	return p, nil
}

func (p *Profiler) cleanPartial() {
	if p.cpuFile != nil {
		pprof.StopCPUProfile()
		_ = p.cpuFile.Close()
		p.cpuFile = nil
	}
}

// Stop 停止所有分析采集并将内存快照写入磁盘
func (p *Profiler) Stop() error {
	var firstErr error

	// 1. 停止 CPU Profiler
	if p.cpuFile != nil {
		pprof.StopCPUProfile()
		if err := p.cpuFile.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
		p.cpuFile = nil
	}

	// 2. 停止 Execution Tracer
	if p.traceFile != nil {
		trace.Stop()
		if err := p.traceFile.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
		p.traceFile = nil
	}

	// 3. 抓取 Memory Heap Profile
	if p.memFilePath != "" {
		f, err := os.Create(p.memFilePath)
		if err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("创建 memprofile 文件失败: %w", err)
			}
		} else {
			runtime.GC() // 显式触发一次 GC 获得最准确的堆分配视图
			if err := pprof.WriteHeapProfile(f); err != nil {
				if firstErr == nil {
					firstErr = fmt.Errorf("写入 heap profile 失败: %w", err)
				}
			}
			_ = f.Close()
		}
	}

	return firstErr
}
