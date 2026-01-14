package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/tadhunt/xblive"
)

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	// Get client ID from environment variable
	clientID := os.Getenv("XBLIVE_CLIENT_ID")
	if clientID == "" {
		fmt.Fprintf(os.Stderr, "Error: XBLIVE_CLIENT_ID environment variable is required\n")
		fmt.Fprintf(os.Stderr, "Set it with: export XBLIVE_CLIENT_ID='your-client-id'\n")
		os.Exit(1)
	}

	// Create client
	client, err := xblive.New(xblive.Config{
		ClientID: clientID,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creating client: %v\n", err)
		os.Exit(1)
	}

	ctx := context.Background()
	command := os.Args[1]

	switch command {
	case "auth":
		handleAuth(ctx, client)
	case "logout":
		handleLogout(ctx, client)
	case "lookup":
		if len(os.Args) < 3 {
			fmt.Fprintf(os.Stderr, "Error: gamertag required\n")
			fmt.Fprintf(os.Stderr, "Usage: %s lookup <gamertag>\n", os.Args[0])
			os.Exit(1)
		}
		handleLookup(ctx, client, os.Args[2])
	case "batch":
		if len(os.Args) < 3 {
			fmt.Fprintf(os.Stderr, "Error: gamertags required\n")
			fmt.Fprintf(os.Stderr, "Usage: %s batch <gamertag1,gamertag2,...>\n", os.Args[0])
			os.Exit(1)
		}
		handleBatch(ctx, client, os.Args[2])
	case "profile":
		if len(os.Args) < 3 {
			fmt.Fprintf(os.Stderr, "Error: gamertag required\n")
			fmt.Fprintf(os.Stderr, "Usage: %s profile <gamertag>\n", os.Args[0])
			os.Exit(1)
		}
		handleProfile(ctx, client, os.Args[2])
	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n", command)
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Printf("Xbox Live API CLI Tool\n\n")
	fmt.Printf("Usage:\n")
	fmt.Printf("  %s <command> [arguments]\n\n", os.Args[0])
	fmt.Printf("Commands:\n")
	fmt.Printf("  auth                    Authenticate with Xbox Live (device code flow)\n")
	fmt.Printf("  logout                  Clear cached authentication tokens\n")
	fmt.Printf("  lookup <gamertag>       Convert a gamertag to XUID\n")
	fmt.Printf("  profile <gamertag>      Get full profile for a gamertag\n")
	fmt.Printf("  batch <gt1,gt2,...>     Convert multiple gamertags to XUIDs\n\n")
	fmt.Printf("Environment Variables:\n")
	fmt.Printf("  XBLIVE_CLIENT_ID        Your Microsoft Entra ID application client ID (required)\n\n")
	fmt.Printf("Examples:\n")
	fmt.Printf("  export XBLIVE_CLIENT_ID='your-client-id'\n")
	fmt.Printf("  %s auth\n", os.Args[0])
	fmt.Printf("  %s lookup MajorNelson\n", os.Args[0])
	fmt.Printf("  %s profile MajorNelson\n", os.Args[0])
	fmt.Printf("  %s batch \"Player1,Player2,Player3\"\n", os.Args[0])
}

func handleAuth(ctx context.Context, client *xblive.Client) {
	fmt.Printf("Starting authentication...\n")
	if err := client.Authenticate(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "Authentication failed: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("✓ Successfully authenticated!\n")
	fmt.Printf("Tokens cached. You can now use lookup commands.\n")
}

func handleLogout(ctx context.Context, client *xblive.Client) {
	if err := client.ClearCache(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to clear cache: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("✓ Successfully logged out and cleared cached tokens.\n")
}

func handleLookup(ctx context.Context, client *xblive.Client, gamertag string) {
	fmt.Printf("Looking up gamertag: %s\n", gamertag)

	profile, err := client.LookupProfileByGamertag(ctx, gamertag)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Lookup failed: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("\n✓ Found!\n")
	fmt.Printf("  Gamertag: %s\n", profile.Gamertag)
	fmt.Printf("  XUID:     %s\n", profile.XUID)
}

func handleProfile(ctx context.Context, client *xblive.Client, gamertag string) {
	fmt.Printf("Looking up profile for gamertag: %s\n", gamertag)

	profile, err := client.LookupProfileByGamertag(ctx, gamertag)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Profile lookup failed: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("\n✓ Profile found!\n\n")

	// Pretty print as JSON
	output, err := json.MarshalIndent(profile, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to format profile: %v\n", err)
		os.Exit(1)
	}
	fmt.Println(string(output))
}

func handleBatch(ctx context.Context, client *xblive.Client, gamertagsStr string) {
	gamertags := strings.Split(gamertagsStr, ",")
	for i, gt := range gamertags {
		gamertags[i] = strings.TrimSpace(gt)
	}

	fmt.Printf("Looking up %d gamertags...\n", len(gamertags))

	results, fuzzyOnly, err := client.GamertagsToXUIDs(ctx, gamertags)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Batch lookup failed: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("\n✓ Results (%d found):\n", len(results))

	// Pretty print as JSON
	output, err := json.MarshalIndent(results, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to format results: %v\n", err)
		os.Exit(1)
	}
	fmt.Println(string(output))

	if len(fuzzyOnly) > 0 {
		fmt.Printf("\n⚠ No exact match (fuzzy results shown): %s\n", strings.Join(fuzzyOnly, ", "))
	}
}
