// bl-manager is a CLI tool for maintaining the DNS blocklist file.
//
// Usage:
//
//	bl-manager [flags] <command> [args]
//
// Commands:
//
//	add <domain>       Add a domain (and optionally its www. subdomain)
//	remove <domain>    Remove a domain from the list
//	list [pattern]     List entries (optional case-insensitive substring filter)
//	check <domain>     Test whether a domain would be blocked (shows matched rule)
//	import <file>      Bulk-import domains from a file (one per line)
//	dedup              Remove duplicates and redundant subdomains, sort in-place
//	stats              Show blocklist statistics
//	export             Write sorted, deduped list to stdout (for piping)
//
// Flags:
//
//	-file string   Blocklist file path (default "blocklist.txt")
package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"
)

var flagFile = flag.String("file", "blocklist.txt", "blocklist file path")

func main() {
	flag.Usage = usage
	flag.Parse()

	args := flag.Args()
	if len(args) == 0 {
		usage()
		os.Exit(1)
	}
	cmd := args[0]
	cmdArgs := args[1:]

	switch cmd {
	case "add":
		if len(cmdArgs) == 0 {
			die("add requires a domain argument")
		}
		cmdAdd(cmdArgs[0])

	case "remove":
		if len(cmdArgs) == 0 {
			die("remove requires a domain argument")
		}
		cmdRemove(cmdArgs[0])

	case "list":
		pattern := ""
		if len(cmdArgs) > 0 {
			pattern = cmdArgs[0]
		}
		cmdList(pattern)

	case "check":
		if len(cmdArgs) == 0 {
			die("check requires a domain argument")
		}
		cmdCheck(cmdArgs[0])

	case "import":
		if len(cmdArgs) == 0 {
			die("import requires a file argument")
		}
		cmdImport(cmdArgs[0])

	case "dedup":
		cmdDedup()

	case "stats":
		cmdStats()

	case "export":
		cmdExport()

	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", cmd)
		usage()
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintf(os.Stderr, `Usage: bl-manager [flags] <command> [args]

Commands:
  add <domain>       Add a domain to the blocklist
  remove <domain>    Remove a domain from the blocklist
  list [pattern]     List entries (optional substring filter)
  check <domain>     Check if a domain would be blocked
  import <file>      Bulk-import domains from a file
  dedup              Remove duplicates / redundant subdomains, sort in-place
  stats              Show blocklist statistics
  export             Write sorted, deduped list to stdout

Flags:
`)
	flag.PrintDefaults()
}

// ---- File I/O helpers ----

// readFile returns the raw lines of the blocklist file, preserving blank
// lines and comment lines so they can be carried through rewrites.
func readFile() ([]string, error) {
	f, err := os.Open(*flagFile)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()

	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	return lines, scanner.Err()
}

// readEntries returns only the normalised domain entries (no comments, no
// blanks) from the blocklist file.
func readEntries() ([]string, error) {
	lines, err := readFile()
	if err != nil {
		return nil, err
	}
	var entries []string
	for _, l := range lines {
		l = strings.TrimSpace(l)
		if l == "" || strings.HasPrefix(l, "#") {
			continue
		}
		entries = append(entries, strings.ToLower(l))
	}
	return entries, nil
}

// writeEntries rewrites the blocklist file with the given entries, preserving
// leading comment lines from the original file.
func writeEntries(entries []string) error {
	// Collect header comments from existing file.
	lines, _ := readFile()
	var header []string
	for _, l := range lines {
		trimmed := strings.TrimSpace(l)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			header = append(header, l)
		} else {
			break // stop at first non-comment, non-blank line
		}
	}

	f, err := os.Create(*flagFile)
	if err != nil {
		return err
	}
	defer f.Close()

	w := bufio.NewWriter(f)
	for _, h := range header {
		fmt.Fprintln(w, h)
	}
	for _, e := range entries {
		fmt.Fprintln(w, e)
	}
	return w.Flush()
}

// appendToFile appends a single domain to the blocklist file.
func appendToFile(domain string) error {
	f, err := os.OpenFile(*flagFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = fmt.Fprintln(f, domain)
	return err
}

// ---- Commands ----

func cmdAdd(domain string) {
	domain = normalise(domain)
	if domain == "" {
		die("invalid domain")
	}

	entries, err := readEntries()
	fatal(err)

	for _, e := range entries {
		if e == domain {
			fmt.Printf("already present: %s\n", domain)
			return
		}
	}

	fatal(appendToFile(domain))
	fmt.Printf("added: %s\n", domain)
}

func cmdRemove(domain string) {
	domain = normalise(domain)

	lines, err := readFile()
	fatal(err)

	var kept []string
	removed := false
	for _, l := range lines {
		trimmed := strings.ToLower(strings.TrimSpace(l))
		if trimmed == domain {
			removed = true
			continue
		}
		kept = append(kept, l)
	}

	if !removed {
		fmt.Printf("not found: %s\n", domain)
		return
	}

	f, err := os.Create(*flagFile)
	fatal(err)
	defer f.Close()

	w := bufio.NewWriter(f)
	for _, l := range kept {
		fmt.Fprintln(w, l)
	}
	fatal(w.Flush())
	fmt.Printf("removed: %s\n", domain)
}

func cmdList(pattern string) {
	entries, err := readEntries()
	fatal(err)

	pattern = strings.ToLower(pattern)
	count := 0
	for _, e := range entries {
		if pattern == "" || strings.Contains(e, pattern) {
			fmt.Println(e)
			count++
		}
	}
	fmt.Fprintf(os.Stderr, "(%d entries)\n", count)
}

func cmdCheck(domain string) {
	domain = normalise(domain)
	entries, err := readEntries()
	fatal(err)

	for _, entry := range entries {
		if domain == entry || strings.HasSuffix(domain, "."+entry) {
			fmt.Printf("BLOCKED  %s  (matched rule: %s)\n", domain, entry)
			return
		}
	}
	fmt.Printf("ALLOWED  %s  (no matching rule)\n", domain)
}

func cmdImport(path string) {
	src, err := os.Open(path)
	fatal(err)
	defer src.Close()

	// Build a set of current entries for dedup.
	existing, err := readEntries()
	fatal(err)
	existSet := make(map[string]bool, len(existing))
	for _, e := range existing {
		existSet[e] = true
	}

	var toAdd []string
	scanner := bufio.NewScanner(src)
	for scanner.Scan() {
		line := normalise(scanner.Text())
		if line == "" || existSet[line] {
			continue
		}
		toAdd = append(toAdd, line)
		existSet[line] = true
	}
	fatal(scanner.Err())

	if len(toAdd) == 0 {
		fmt.Println("no new entries to add")
		return
	}

	f, err := os.OpenFile(*flagFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	fatal(err)
	defer f.Close()

	w := bufio.NewWriter(f)
	for _, d := range toAdd {
		fmt.Fprintln(w, d)
	}
	fatal(w.Flush())
	fmt.Printf("imported %d new entries\n", len(toAdd))
}

// cmdDedup removes exact duplicates and redundant subdomain entries, then
// sorts and rewrites the file in-place.
//
// A "redundant subdomain" entry is one where a shorter parent domain already
// appears in the list (e.g. "sub.example.com" is redundant when "example.com"
// is present).
func cmdDedup() {
	entries, err := readEntries()
	fatal(err)

	before := len(entries)

	// 1. Deduplicate (case-insensitive).
	seen := map[string]bool{}
	var unique []string
	for _, e := range entries {
		if !seen[e] {
			seen[e] = true
			unique = append(unique, e)
		}
	}

	// 2. Sort so shorter (parent) domains come first.
	sort.Slice(unique, func(i, j int) bool {
		if len(unique[i]) != len(unique[j]) {
			return len(unique[i]) < len(unique[j])
		}
		return unique[i] < unique[j]
	})

	// 3. Remove entries covered by a shorter parent.
	var final []string
	for _, e := range unique {
		if !coveredByAny(e, final) {
			final = append(final, e)
		}
	}

	// 4. Final alphabetical sort for readability.
	sort.Strings(final)

	fatal(writeEntries(final))
	fmt.Printf("dedup complete: %d → %d entries (removed %d)\n",
		before, len(final), before-len(final))
}

// coveredByAny returns true when any entry in parents is a suffix-ancestor of domain.
func coveredByAny(domain string, parents []string) bool {
	for _, p := range parents {
		if domain == p || strings.HasSuffix(domain, "."+p) {
			return true
		}
	}
	return false
}

func cmdStats() {
	entries, err := readEntries()
	fatal(err)

	// Count top-level domains.
	tldCounts := map[string]int{}
	for _, e := range entries {
		parts := strings.Split(e, ".")
		if len(parts) >= 2 {
			tld := parts[len(parts)-1]
			tldCounts[tld]++
		}
	}

	type kv struct{ k string; v int }
	var sorted []kv
	for k, v := range tldCounts {
		sorted = append(sorted, kv{k, v})
	}
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].v > sorted[j].v })

	fmt.Printf("=== Blocklist Statistics: %s ===\n", *flagFile)
	fmt.Printf("Total entries:   %d\n", len(entries))
	fmt.Println()
	fmt.Println("Top TLDs:")
	limit := 10
	for i, kv := range sorted {
		if i >= limit {
			break
		}
		fmt.Printf("  .%-10s  %d\n", kv.k, kv.v)
	}
}

func cmdExport() {
	entries, err := readEntries()
	fatal(err)

	// Dedup in memory without writing back.
	seen := map[string]bool{}
	var unique []string
	for _, e := range entries {
		if !seen[e] {
			seen[e] = true
			unique = append(unique, e)
		}
	}

	// Remove covered subdomains, sort.
	sort.Slice(unique, func(i, j int) bool {
		if len(unique[i]) != len(unique[j]) {
			return len(unique[i]) < len(unique[j])
		}
		return unique[i] < unique[j]
	})
	var final []string
	for _, e := range unique {
		if !coveredByAny(e, final) {
			final = append(final, e)
		}
	}
	sort.Strings(final)

	for _, e := range final {
		fmt.Println(e)
	}
}

// ---- Helpers ----

func normalise(domain string) string {
	domain = strings.ToLower(strings.TrimSpace(domain))
	// Strip scheme if accidentally passed.
	for _, prefix := range []string{"http://", "https://"} {
		domain = strings.TrimPrefix(domain, prefix)
	}
	// Strip trailing dot and path.
	domain = strings.TrimSuffix(domain, ".")
	if idx := strings.IndexByte(domain, '/'); idx >= 0 {
		domain = domain[:idx]
	}
	return domain
}

func fatal(err error) {
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func die(msg string) {
	fmt.Fprintf(os.Stderr, "error: %s\n", msg)
	os.Exit(1)
}
