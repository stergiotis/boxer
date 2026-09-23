// Package deadletter is where a stevedore lander or driver records what it
// gave up on (ADR-0252 §SD3): one row of the stevedoreDeadLetter kind per
// message or request, through the facts-bound generated store, or a log
// line where no store is at hand. It is the one row stevedore writes on its
// own account; every other row is the application sink's.
package deadletter
