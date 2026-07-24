// Package driver is the compiled-in driver registry boundary.
//
// Drivers implement core contracts and register at process start. Product
// packages outside this tree must not import a concrete vendor driver.
package driver
