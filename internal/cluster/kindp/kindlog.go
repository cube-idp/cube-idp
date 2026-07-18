package kindp

import (
	"fmt"

	kindlog "sigs.k8s.io/kind/pkg/log"
)

// kindLogger adapts kind's log.Logger to a plain line sink so `up` can
// stream cluster provisioning into the StepLog event channel. Verbosity
// >0 is dropped: kind's V(0) is its user-facing progress narration.
type kindLogger struct{ sink func(string) }

func newKindLogger(sink func(string)) kindLogger { return kindLogger{sink: sink} }

func (k kindLogger) Warn(message string)               { k.sink(message) }
func (k kindLogger) Warnf(format string, args ...any)  { k.sink(sprintf(format, args...)) }
func (k kindLogger) Error(message string)              { k.sink(message) }
func (k kindLogger) Errorf(format string, args ...any) { k.sink(sprintf(format, args...)) }
func (k kindLogger) V(level kindlog.Level) kindlog.InfoLogger {
	if level > 0 {
		return nopInfo{}
	}
	return infoLogger{sink: k.sink}
}

type infoLogger struct{ sink func(string) }

func (i infoLogger) Info(message string)              { i.sink(message) }
func (i infoLogger) Infof(format string, args ...any) { i.sink(sprintf(format, args...)) }
func (i infoLogger) Enabled() bool                    { return true }

type nopInfo struct{}

func (nopInfo) Info(string)          {}
func (nopInfo) Infof(string, ...any) {}
func (nopInfo) Enabled() bool        { return false }

func sprintf(format string, args ...any) string { return fmt.Sprintf(format, args...) }
