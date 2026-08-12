package configreload

import "errors"

var (
	errConfigDirRequired = errors.New("configreload: config dir is required")
	errOnChangeRequired  = errors.New("configreload: OnChange is required")
)
