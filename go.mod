// reqmin — shrinks an HTTP request to the minimal headers and params
// that still reproduce, using delta debugging.
//
// version:    0.1.0
// author:     JaydenCJ
// license:    MIT
// repository: https://github.com/JaydenCJ/reqmin
// keywords:   http, delta-debugging, curl, minimizer, debugging, api, cli
//
// Zero runtime dependencies: standard library only.
module github.com/JaydenCJ/reqmin

go 1.22
