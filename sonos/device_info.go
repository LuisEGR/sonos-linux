package sonos

import (
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
)

type deviceRoot struct {
	Device deviceDesc `xml:"device"`
}

type deviceDesc struct {
	ModelName       string `xml:"modelName"`
	ModelNumber     string `xml:"modelNumber"`
	SerialNum       string `xml:"serialNum"`
	SoftwareVersion string `xml:"softwareVersion"`
	HardwareVersion string `xml:"hardwareVersion"`
}

// FetchDeviceInfo populates model/serial/version info from the device description XML.
func FetchDeviceInfo(dev *Device) error {
	resp, err := http.Get(fmt.Sprintf("http://%s:1400/xml/device_description.xml", dev.IP))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	var root deviceRoot
	if err := xml.Unmarshal(body, &root); err != nil {
		return err
	}

	dev.ModelName = root.Device.ModelName
	dev.ModelNumber = root.Device.ModelNumber
	dev.SerialNumber = root.Device.SerialNum
	dev.SoftwareVersion = root.Device.SoftwareVersion
	dev.HardwareVersion = root.Device.HardwareVersion
	return nil
}
