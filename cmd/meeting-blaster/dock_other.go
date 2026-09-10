//go:build !darwin

package main

// Only macOS has an activation policy to get wrong.
func respectBundleActivationPolicy() {}
