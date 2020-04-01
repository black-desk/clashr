package auth

import (
	"github.com/black-desk/clashr/component/auth"
)

var (
	authenticator auth.Authenticator
)

func Authenticator() auth.Authenticator {
	return authenticator
}

func SetAuthenticator(au auth.Authenticator) {
	authenticator = au
}
