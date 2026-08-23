package entity

import "regexp"

var ticketQuery = regexp.MustCompile(`(ticket=)[A-Za-z0-9._~+/=-]+`)

func Redacted(text string) string {
	return ticketQuery.ReplaceAllString(text, "${1}…")
}
