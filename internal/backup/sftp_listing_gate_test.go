package backup

import (
	"encoding/binary"
	"io"
	"os"
	"os/exec"
	"testing"
	"time"
)

// Invoked only as the dedicated sshd subsystem in the disposable native fixture.
// Preserve real SFTP traffic byte-for-byte except for pausing one OPENDIR request
// until the parent stops sshd. No diagnostic text is manufactured by this gate.
func TestSFTPListingProtocolGate(t *testing.T) {
	if os.Args[0] != "/fixture/sftp-gate" {
		t.Skip("SFTP fixture subprocess only")
	}
	server := exec.Command("/usr/lib/openssh/sftp-server")
	input, err := server.StdinPipe()
	if err != nil {
		os.Exit(1)
	}
	server.Stdout, server.Stderr = os.Stdout, os.Stderr
	if server.Start() != nil {
		os.Exit(1)
	}
	for {
		var header [4]byte
		if _, err := io.ReadFull(os.Stdin, header[:]); err != nil {
			break
		}
		length := binary.BigEndian.Uint32(header[:])
		if length > 16<<20 {
			os.Exit(1)
		}
		packet := make([]byte, length)
		if _, err := io.ReadFull(os.Stdin, packet); err != nil {
			os.Exit(1)
		}
		// SSH_FXP_OPENDIR: type(1), request ID(4), path length(4), path.
		if len(packet) >= 9 && packet[0] == 11 && int(binary.BigEndian.Uint32(packet[5:9])) == len(packet)-9 && string(packet[9:]) == "/fixture/repository/data" {
			if _, err := os.Stat("/fixture/arm-listing"); err == nil {
				if os.WriteFile("/fixture/listing-reached", nil, 0600) != nil {
					os.Exit(1)
				}
				for {
					time.Sleep(time.Second)
				} // Terminated with the owned sshd.
			}
		}
		if _, err := input.Write(append(header[:], packet...)); err != nil {
			os.Exit(1)
		}
	}
	_ = input.Close()
	if server.Wait() != nil {
		os.Exit(1)
	}
	os.Exit(0) // Never emit the Go test runner's PASS line on the SFTP channel.
}
