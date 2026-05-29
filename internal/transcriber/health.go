package transcriber

import "context"

type HealthChecker interface {
	Check(context.Context) error
}

func Check(ctx context.Context, t Transcriber) error {
	checker, ok := t.(HealthChecker)
	if !ok {
		return nil
	}
	return checker.Check(ctx)
}
