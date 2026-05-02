//go:build !desktop

package authapi

import nethttp "net/http"

func registerLocalSessionRoute(_ *nethttp.ServeMux, _ Deps) {}
