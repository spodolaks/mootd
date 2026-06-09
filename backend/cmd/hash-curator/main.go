// Throwaway helper: hash a password with the same argon2id parameters
// the admin package uses, so we can hand-craft a mongosh insert for a
// new admin without going through the (not-yet-built) admin-management
// endpoint. Delete this file once the proper /admins management UI ships.
package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"mootd/backend/internal/admin"
)

func main() {
	// Read the password from stdin rather than os.Args so it never lands in
	// shell history or /proc/<pid>/cmdline (where any local user could read it).
	// Usage: `printf '%s' "$PW" | hash-curator`, or type it and press Enter.
	fmt.Fprintln(os.Stderr, "reading password from stdin...")
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		fmt.Fprintln(os.Stderr, "read password:", err)
		os.Exit(2)
	}
	// Trim a single trailing newline (handles both "\n" and "\r\n") while
	// preserving any other whitespace the password might legitimately contain.
	password := strings.TrimRight(line, "\r\n")
	if password == "" {
		fmt.Fprintln(os.Stderr, "error: empty password on stdin")
		os.Exit(2)
	}
	hash, err := admin.HashPassword(password)
	if err != nil {
		fmt.Fprintln(os.Stderr, "hash:", err)
		os.Exit(1)
	}
	fmt.Println(hash)
}
