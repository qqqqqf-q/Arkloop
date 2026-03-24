package data

import "errors"

var ErrThreadBusy = errors.New("thread has active root run")
