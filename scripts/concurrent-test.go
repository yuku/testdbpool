package main

import (
	"fmt"
	"os"
	"os/exec"
	"sync"
	"time"
)

func main() {
	fmt.Println("=== Concurrent Test Runner ===")

	// Test scenarios
	scenarios := []struct {
		name     string
		packages []string
		parallel int
	}{
		{
			name: "Concurrent package execution",
			packages: []string{
				"./examples/pgxpool/package1",
				"./examples/pgxpool/package2",
				"./examples/pgxpool/package3",
			},
			parallel: 3,
		},
		{
			name: "Rapid sequential execution",
			packages: []string{
				"./examples/pgxpool/...",
			},
			parallel: 1,
		},
	}

	for _, scenario := range scenarios {
		fmt.Printf("\n--- Running: %s ---\n", scenario.name)
		runScenario(scenario.packages, scenario.parallel)
		time.Sleep(2 * time.Second)
	}
}

func runScenario(packages []string, parallel int) {
	var wg sync.WaitGroup
	semaphore := make(chan struct{}, parallel)

	for i := 0; i < 5; i++ { // Run 5 iterations
		for _, pkg := range packages {
			wg.Add(1)
			semaphore <- struct{}{}

			go func(pkg string, iteration int) {
				defer wg.Done()
				defer func() { <-semaphore }()

				cmd := exec.Command("go", "test", "-v", "-race", pkg)
				cmd.Env = append(os.Environ(),
					fmt.Sprintf("TEST_ITERATION=%d", iteration),
				)

				output, err := cmd.CombinedOutput()
				if err != nil {
					fmt.Printf("FAIL: Iteration %d, Package %s\n", iteration, pkg)
					fmt.Printf("Error: %v\n", err)
					fmt.Printf("Output:\n%s\n", string(output))
				} else {
					fmt.Printf("PASS: Iteration %d, Package %s\n", iteration, pkg)
				}
			}(pkg, i)
		}
	}

	wg.Wait()
}
