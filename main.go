// rf-control's root command remains a lightweight CLI-only entry point for
// development and non-desktop builds. The packaged desktop executable imports
// the same internal CLI and dispatches to it whenever arguments are supplied.
package main

import "github.com/OcupointInc/rf-control/internal/cli"

func main() { cli.Main() }
