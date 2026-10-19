package execution

import "errors"

type writeProgress struct {
	error
	confirmed int
	stage     string
}

func (e writeProgress) Unwrap() error { return e.error }

// WithWriteProgress retains confirmed bytes when file activation fails.
func WithWriteProgress(err error, confirmed int, stage string) error {
	if err == nil {
		return nil
	}
	return writeProgress{err, confirmed, stage}
}

// WriteProgress returns confirmed bytes and the failed operation stage.
func WriteProgress(err error) (int, string, bool) {
	var progress writeProgress
	if !errors.As(err, &progress) {
		return 0, "", false
	}
	return progress.confirmed, progress.stage, true
}
