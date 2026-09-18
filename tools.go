//go:build tools

// Package main pins module dependencies that are shared by all internal packages
// so that `go mod tidy` keeps them even before every package imports them.
package main

import (
	_ "github.com/caarlos0/env/v11"
	_ "github.com/coder/websocket"
	_ "github.com/danielgtaylor/huma/v2"
	_ "github.com/danielgtaylor/huma/v2/adapters/humachi"
	_ "github.com/go-chi/chi/v5"
	_ "github.com/go-chi/chi/v5/middleware"
	_ "github.com/google/uuid"
	_ "github.com/pressly/goose/v3"
	_ "github.com/stretchr/testify/require"
	_ "golang.org/x/sys/unix"
	_ "golang.org/x/text/unicode/norm"
	_ "gopkg.in/yaml.v3"
	_ "modernc.org/sqlite"
)
