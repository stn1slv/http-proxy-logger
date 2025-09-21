package main

import (
	"flag"
	"os"
	"testing"
)

func TestNoColorFlagParsing(t *testing.T) {
	// Reset flag for testing
	flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ExitOnError)
	
	// Re-declare the no-color flag
	testNoColor := flag.Bool("no-color", false, "disable colored output")
	
	// Test case 1: no-color flag not provided (default false)
	os.Args = []string{"http-proxy-logger"}
	flag.Parse()
	
	if *testNoColor != false {
		t.Errorf("Expected no-color to default to false, got %v", *testNoColor)
	}
	
	// Reset for next test
	flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ExitOnError)
	testNoColor = flag.Bool("no-color", false, "disable colored output")
	
	// Test case 2: no-color flag explicitly set to true
	os.Args = []string{"http-proxy-logger", "-no-color=true"}
	flag.Parse()
	
	if *testNoColor != true {
		t.Errorf("Expected no-color to be true when set, got %v", *testNoColor)
	}
	
	// Reset for next test
	flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ExitOnError)
	testNoColor = flag.Bool("no-color", false, "disable colored output")
	
	// Test case 3: no-color flag provided without value (should be true)
	os.Args = []string{"http-proxy-logger", "-no-color"}
	flag.Parse()
	
	if *testNoColor != true {
		t.Errorf("Expected no-color to be true when flag provided without value, got %v", *testNoColor)
	}
}