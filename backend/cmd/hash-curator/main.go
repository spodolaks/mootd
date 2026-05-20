// Throwaway helper: hash a password with the same argon2id parameters
// the admin package uses, so we can hand-craft a mongosh insert for a
// new admin without going through the (not-yet-built) admin-management
// endpoint. Delete this file once the proper /admins management UI ships.
package main

import (
	"fmt"
	"os"

	"mootd/backend/internal/admin"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: hash-curator <password>")
		os.Exit(2)
	}
	hash, err := admin.HashPassword(os.Args[1])
	if err != nil {
		fmt.Fprintln(os.Stderr, "hash:", err)
		os.Exit(1)
	}
	fmt.Println(hash)
}
