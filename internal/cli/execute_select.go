package cli

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/leightonvanrooijen/utopia/internal/infra/storage"
)

// selectChangeRequest lists available CRs and prompts the user to select one.
func selectChangeRequest(store *storage.YAMLStore) (string, error) {
	crs, err := store.ListChangeRequests()
	if err != nil {
		return "", fmt.Errorf("failed to list change requests: %w", err)
	}

	if len(crs) == 0 {
		return "", fmt.Errorf("no change requests found in .utopia/change-requests/\n\nCreate one with: utopia cr")
	}

	fmt.Println("Available change requests:")
	fmt.Println()
	for i, cr := range crs {
		fmt.Printf("  [%d] %s\n", i+1, cr.Title)
	}
	fmt.Println()

	reader := bufio.NewReader(os.Stdin)
	fmt.Print("Select a change request (number): ")
	input, err := reader.ReadString('\n')
	if err != nil {
		return "", fmt.Errorf("failed to read input: %w", err)
	}

	input = strings.TrimSpace(input)
	selection, err := strconv.Atoi(input)
	if err != nil || selection < 1 || selection > len(crs) {
		return "", fmt.Errorf("invalid selection: %s (enter a number between 1 and %d)", input, len(crs))
	}

	selectedCR := crs[selection-1]
	fmt.Printf("\nSelected: %s\n\n", selectedCR.Title)

	return selectedCR.ID, nil
}
