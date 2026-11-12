// Package middleware holds the edge middleware of the satellite's OCI proxy
// (plan item 9.3, ADR-0019): authentication of non-loopback clients and pull
// metrics. Stack gives the WrapAll order with edge admission (plan item 7.5).
package middleware
