package tfprovider

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"

	gigahost "github.com/kradalby/gigahost-go/client"
)

// TestDNSValueForState covers the type-aware trailing-dot convergence used when
// refreshing a record. Hostname-valued types keep the configured (dotless) form
// when the API only adds a trailing dot; content-valued types (TXT, CAA) must
// preserve the API form verbatim because the dot is significant.
func TestDNSValueForState(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		prior      types.String
		apiValue   string
		recordType gigahost.RecordType
		want       string
	}{
		{
			name:       "import (null prior) stores API form with dot",
			prior:      types.StringNull(),
			apiValue:   "target.example.no.",
			recordType: gigahost.RecordTypeCNAME,
			want:       "target.example.no.",
		},
		{
			name:       "import (unknown prior) stores API form",
			prior:      types.StringUnknown(),
			apiValue:   "ns1.example.no.",
			recordType: gigahost.RecordTypeNS,
			want:       "ns1.example.no.",
		},
		{
			name:       "CNAME differs only by trailing dot keeps prior dotless form",
			prior:      types.StringValue("target.example.no"),
			apiValue:   "target.example.no.",
			recordType: gigahost.RecordTypeCNAME,
			want:       "target.example.no",
		},
		{
			name:       "MX differs only by trailing dot keeps prior form",
			prior:      types.StringValue("mail.example.no"),
			apiValue:   "mail.example.no.",
			recordType: gigahost.RecordTypeMX,
			want:       "mail.example.no",
		},
		{
			name:       "NS differs only by trailing dot keeps prior form",
			prior:      types.StringValue("ns1.example.no"),
			apiValue:   "ns1.example.no.",
			recordType: gigahost.RecordTypeNS,
			want:       "ns1.example.no",
		},
		{
			name:       "SRV differs only by trailing dot keeps prior form",
			prior:      types.StringValue("0 5 443 svc.example.no"),
			apiValue:   "0 5 443 svc.example.no.",
			recordType: gigahost.RecordTypeSRV,
			want:       "0 5 443 svc.example.no",
		},
		{
			name:       "PTR differs only by trailing dot keeps prior form",
			prior:      types.StringValue("host.example.no"),
			apiValue:   "host.example.no.",
			recordType: gigahost.RecordTypePTR,
			want:       "host.example.no",
		},
		{
			name:       "exact match keeps API form",
			prior:      types.StringValue("target.example.no."),
			apiValue:   "target.example.no.",
			recordType: gigahost.RecordTypeCNAME,
			want:       "target.example.no.",
		},
		{
			// The dot is content for TXT: a dot-only difference must NOT be
			// treated as equal; store the API form verbatim.
			name:       "TXT trailing dot is significant, stores API form",
			prior:      types.StringValue("v=spf1 -all"),
			apiValue:   "v=spf1 -all.",
			recordType: gigahost.RecordTypeTXT,
			want:       "v=spf1 -all.",
		},
		{
			name:       "TXT exact match stays",
			prior:      types.StringValue("hello"),
			apiValue:   "hello",
			recordType: gigahost.RecordTypeTXT,
			want:       "hello",
		},
		{
			// CAA is content-valued too; dot difference is real.
			name:       "CAA trailing dot is significant, stores API form",
			prior:      types.StringValue("0 issue letsencrypt.org"),
			apiValue:   "0 issue letsencrypt.org.",
			recordType: gigahost.RecordTypeCAA,
			want:       "0 issue letsencrypt.org.",
		},
		{
			// A genuine content change (not just a dot) always takes the API form.
			name:       "CNAME genuine change stores API form",
			prior:      types.StringValue("old.example.no"),
			apiValue:   "new.example.no.",
			recordType: gigahost.RecordTypeCNAME,
			want:       "new.example.no.",
		},
		{
			name:       "A record value unaffected, stores API form",
			prior:      types.StringValue("1.2.3.4"),
			apiValue:   "1.2.3.5",
			recordType: gigahost.RecordTypeA,
			want:       "1.2.3.5",
		},
		{
			// The servers API spells IPv6 with "::" for one zero group; the DNS
			// API stores RFC 5952. Same address, so keep the configured form.
			name:       "AAAA same address different spelling keeps prior form",
			prior:      types.StringValue("2001:db8:ffff:1:2:3::195"),
			apiValue:   "2001:db8:ffff:1:2:3:0:195",
			recordType: gigahost.RecordTypeAAAA,
			want:       "2001:db8:ffff:1:2:3::195",
		},
		{
			name:       "AAAA genuine change stores API form",
			prior:      types.StringValue("2001:db8::1"),
			apiValue:   "2001:db8::2",
			recordType: gigahost.RecordTypeAAAA,
			want:       "2001:db8::2",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := dnsValueForState(tt.prior, tt.apiValue, tt.recordType)
			if got != tt.want {
				t.Errorf("dnsValueForState(%v, %q, %q) = %q, want %q",
					tt.prior, tt.apiValue, tt.recordType, got, tt.want)
			}
		})
	}
}

// TestMatchRecordIPv6Spelling is the "record not found after create" bug: a
// record created from gigahost_server.ipv6 ("…:3::195") is stored by the DNS
// API as RFC 5952 ("…:3:0:195"), and a string compare never finds it.
func TestMatchRecordIPv6Spelling(t *testing.T) {
	t.Parallel()

	records := []gigahost.DNSRecord{
		{ID: "a", Name: "vpn", Type: gigahost.RecordTypeA, Value: "192.0.2.11"},
		{ID: "aaaa", Name: "vpn", Type: gigahost.RecordTypeAAAA, Value: "2001:db8:ffff:1:2:3:0:195"},
	}

	got := matchRecord(records, "vpn", "AAAA", "2001:db8:ffff:1:2:3::195")
	if got == nil || got.ID != "aaaa" {
		t.Fatalf("matchRecord = %v, want the AAAA record stored in RFC 5952 form", got)
	}
}
