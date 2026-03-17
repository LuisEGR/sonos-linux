package sonos

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// soapArg is an ordered name-value pair for SOAP arguments.
// UPnP requires arguments in the order defined by the service description.
type soapArg struct {
	Name  string
	Value string
}

func soapCall(deviceIP, controlURL, serviceType, action string, args []soapArg) (map[string]string, error) {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0"?>`)
	b.WriteString(`<s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/"`)
	b.WriteString(` s:encodingStyle="http://schemas.xmlsoap.org/soap/encoding/">`)
	b.WriteString(`<s:Body>`)
	fmt.Fprintf(&b, `<u:%s xmlns:u="urn:schemas-upnp-org:service:%s:1">`, action, serviceType)
	for _, arg := range args {
		fmt.Fprintf(&b, "<%s>%s</%s>", arg.Name, xmlEscape(arg.Value), arg.Name)
	}
	fmt.Fprintf(&b, `</u:%s>`, action)
	b.WriteString(`</s:Body>`)
	b.WriteString(`</s:Envelope>`)

	url := fmt.Sprintf("http://%s:1400%s", deviceIP, controlURL)
	req, err := http.NewRequest("POST", url, strings.NewReader(b.String()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", `text/xml; charset="utf-8"`)
	req.Header.Set("SOAPACTION", fmt.Sprintf("urn:schemas-upnp-org:service:%s:1#%s", serviceType, action))

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("SOAP error %d: %s", resp.StatusCode, string(respBody))
	}

	return parseSoapResponse(respBody)
}

func parseSoapResponse(data []byte) (map[string]string, error) {
	result := make(map[string]string)
	decoder := xml.NewDecoder(bytes.NewReader(data))

	var current *struct {
		name string
		data strings.Builder
	}

	for {
		token, err := decoder.Token()
		if err != nil {
			break
		}

		switch t := token.(type) {
		case xml.StartElement:
			current = &struct {
				name string
				data strings.Builder
			}{name: t.Name.Local}
		case xml.CharData:
			if current != nil {
				current.data.Write(t)
			}
		case xml.EndElement:
			if current != nil && current.name == t.Name.Local {
				val := strings.TrimSpace(current.data.String())
				if val != "" {
					result[current.name] = val
				}
			}
			current = nil
		}
	}

	return result, nil
}

func xmlEscape(s string) string {
	var buf bytes.Buffer
	xml.EscapeText(&buf, []byte(s))
	return buf.String()
}
