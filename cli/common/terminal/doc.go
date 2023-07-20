// Package terminal is a modified version of
// https://github.com/hashicorp/waypoint-plugin-sdk/tree/74d9328929293551499078da388b8d057f3b2341/terminal.
//
// This terminal package only contains the basic UI implementation and excludes the glint UI and noninteractive UI
// implementations as they do not yet have an Input implementation which we leverage in commands.
//
// This terminal package does not contain the Status, Step, and StepGroup interface and implementation from the original
// terminal package since using the spinner package does not allow streaming outputs of a given Step or Status. Instead,
// we only use the UI interface to standardize Input and Output formatting.
package terminal
