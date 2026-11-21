// Command tinio runs Tinio on your own machine: the API, every agent, and
// (soon) the app, in one process, with everything in one SQLite file.
package main

import "github.com/Grandbusta/tinio/api"

func main() { api.Main() }
