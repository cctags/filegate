package gate

import (
	"crypto/sha256"
	"encoding/base64"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// This fixed program neither reads credentials/URLs nor submits forms. No
// dynamic data is interpolated into it; its exact bytes are allowed by CSP.
const clientTimezoneScript = `(()=>{"use strict";for(const form of document.forms){if(form.method.toLowerCase()!=="post")continue;form.addEventListener("submit",()=>{try{const zone=Intl.DateTimeFormat().resolvedOptions().timeZone;const offset=String(-new Date().getTimezoneOffset());for(const [name,value] of [["client_timezone",zone],["client_utc_offset",offset]]){let field=form.querySelector('input[name="'+name+'"]');if(!field){field=document.createElement("input");field.type="hidden";field.name=name;form.appendChild(field);}field.value=value;}}catch(_){}});}})();`

var clientTimezoneCSP = func() string {
	sum := sha256.Sum256([]byte(clientTimezoneScript))
	return baseCSP + "; script-src 'sha256-" + base64.StdEncoding.EncodeToString(sum[:]) + "'"
}()

func reportedTimezone(value string) string {
	if len(value) == 0 || len(value) > 64 || value == "Local" || strings.HasPrefix(value, "/") {
		return "unknown"
	}
	for _, c := range value {
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || strings.ContainsRune("/_+-", c)) {
			return "unknown"
		}
	}
	// Use the local IANA database to avoid logging arbitrary, attacker-chosen
	// strings. Missing server tzdata degrades to unknown, never an auth failure.
	if _, err := time.LoadLocation(value); err != nil {
		return "unknown"
	}
	return value
}

func updateClientTimezone(r *http.Request) {
	audit := auditFor(r)
	if audit == nil {
		return
	}
	zone, hasZone := r.PostForm["client_timezone"]
	offset, hasOffset := r.PostForm["client_utc_offset"]
	if !hasZone && !hasOffset {
		return
	}
	audit.timezone = "unknown"
	audit.utcOffset = "unknown"
	audit.timezoneSource = "form_reported"
	if len(zone) == 1 {
		audit.timezone = reportedTimezone(zone[0])
	}
	if hasZone && audit.timezone == "unknown" {
		audit.timezoneSource = "unverified_form"
	}
	if len(offset) == 1 {
		number, err := strconv.Atoi(offset[0])
		if err == nil && number >= -840 && number <= 840 {
			audit.utcOffset = number
		} else {
			audit.timezoneSource = "unverified_form"
		}
	} else if hasOffset {
		audit.timezoneSource = "unverified_form"
	}
}

func clientAttributes(r *http.Request) []any {
	if audit := auditFor(r); audit != nil {
		attrs := append([]any{}, audit.attrs...)
		return append(attrs, "timezone", audit.timezone, "timezone_source", audit.timezoneSource, "utc_offset_minutes", audit.utcOffset)
	}
	return append(clientFields(r), "timezone", "unknown", "timezone_source", "unavailable", "utc_offset_minutes", "unknown")
}
