package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/url"
	"os"
	"os/exec"
	"path"
	"strings"
	"time"

	"github.com/gocarina/gocsv"
)

var (
	OpenVPNConfig = "/etc/openvpn"
	EasyRSAPKI    = OpenVPNConfig + "/pki"
)

var (
	Hostname      = "udp://localhost:1194"
	BinPath       = "/usr/local/bin/"
	DockerCommand []string // = []string{"docker", "compose", "-f", "compose.koco.yaml", "exec", "koco"}
)

func BuildCommand(cmd string, args ...string) (string, []string) {
	if len(DockerCommand) > 0 {
		return DockerCommand[0], append(append(DockerCommand[1:], cmd), args...)
	}
	return BinPath + cmd, args
}

func init() {
	OpenVPN := os.Getenv("OPENVPN")
	EasyrsaPki := os.Getenv("EASYRSA_PKI")

	if OpenVPN != "" {
		OpenVPNConfig = OpenVPN
	}

	if EasyrsaPki != "" {
		EasyRSAPKI = EasyrsaPki
	}

	OvpnDockerExecCommand := os.Getenv("OVPN_DOCKER_EXEC_COMMAND")
	OvpnBinPath := os.Getenv("OVPN_BIN_PATH")

	if OvpnDockerExecCommand != "" {
		DockerCommand = strings.Split(OvpnDockerExecCommand, " ")
	}
	if OvpnBinPath != "" {
		BinPath = OvpnBinPath
	}
}

func FlashExec(cmd string, args ...string) (string, error) {
	log.Println("Run command:", cmd, args)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, cmd, args...)
	output, err := command.CombinedOutput()
	if err != nil {
		return "", err
	}
	return string(output), nil
}

type OutputPayload struct {
	Message string
	Channel int
}

func InteractableExec(onColon func(channel int, line string) string, cmd string, args ...string) error {
	log.Println("Run interactable command:", cmd, args)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	command := exec.CommandContext(ctx, cmd, args...)

	stdin, err := command.StdinPipe()
	if err != nil {
		return err
	}
	stdout, err := command.StdoutPipe()
	if err != nil {
		return err
	}
	stderr, err := command.StderrPipe()
	if err != nil {
		return err
	}

	var (
		done         = false
		outputChan   = make(chan OutputPayload)
		asyncErrChan = make(chan error)
	)

	defer func() {
		done = true
		close(outputChan)
		close(asyncErrChan)
	}()

	go func() {
		outputCache := []string{
			"",
			"",
			"",
		}
		for {
			payload, ok := <-outputChan
			if !ok || done {
				break
			}
			if payload.Channel == 1 {
				_, _ = fmt.Fprint(os.Stdout, payload.Message)
			} else {
				_, _ = fmt.Fprint(os.Stderr, payload.Message)
			}
			outputCache[payload.Channel] += payload.Message
			if strings.HasSuffix(strings.TrimSpace(outputCache[payload.Channel]), ":") {
				lines := strings.Split(outputCache[payload.Channel], "\n")
				input := onColon(1, strings.TrimSpace(lines[len(lines)-1]))
				if input != "" {
					outputCache[payload.Channel] = ""
					_, err := stdin.Write([]byte(input))
					if err != nil {
						if !done {
							asyncErrChan <- err
						}
						break
					}
				}
			}
		}
	}()

	listenStdout := func(reader io.Reader, channelIndex int) {
		go func() {
			defer func() {
				// may emit "send data into a closed channel"
				_ = recover()
			}()
			eof := false
			buffer := make([]byte, 1024)
			for {
				n, err := reader.Read(buffer)
				if err != nil {
					if err == io.EOF {
						eof = true
					} else {
						if !done {
							asyncErrChan <- err
						}
						break
					}
				}

				buffer := buffer[:n]
				if done {
					break
				}
				outputChan <- OutputPayload{
					// FIXME this can not work on non-ascii string,
					// 		 because the bytes of a word might be split at the buffer boundary
					Message: string(buffer),
					Channel: channelIndex,
				}

				if eof || done {
					if !done {
						asyncErrChan <- nil
					}
					return
				}
			}
		}()
	}

	listenStdout(stdout, 1)
	listenStdout(stderr, 2)

	err = command.Start()
	if err != nil {
		return err
	}

	select {
	case err := <-asyncErrChan:
		if err != nil {
			return err
		}
	case <-ctx.Done():
		if ctx.Err() != nil {
			return ctx.Err()
		}
	}

	return nil
}

type Client struct {
	Name   string `csv:"name"`
	Begin  string `csv:"begin"`
	End    string `csv:"end"`
	Status string `csv:"status"`

	// ccd
	Config string `csv:"-"`
}

func ListClients() ([]*Client, error) {
	var clients []*Client
	cmd, args := BuildCommand("ovpn_listclients")
	csv, err := FlashExec(cmd, args...)
	if err != nil {
		return nil, err
	}
	if err := gocsv.UnmarshalString(csv, &clients); err != nil {
		return nil, err
	}

	// scan ccd
	for _, client := range clients {
		content, err := GetClientConfig(client.Name)
		if err == nil && content != "" {
			client.Config = strings.TrimSpace(content)
		}
	}

	return clients, nil
}

func GetClientConfig(name string) (string, error) {
	clientConfigFilePath := path.Join(OpenVPNConfig, "ccd", name)
	_, err := os.Stat(clientConfigFilePath)
	if err != nil {
		return "", err
	}
	content, err := os.ReadFile(clientConfigFilePath)
	return string(content), err
}

func SetClientConfig(name, config string) error {
	config = strings.TrimSpace(config)
	clientConfigFilePath := path.Join(OpenVPNConfig, "ccd", name)
	err := os.WriteFile(clientConfigFilePath, []byte(config), 0o666)
	return err
}

func BuildClientFull(capass, name, pass string) error {
	args := []string{"build-client-full", name}
	if pass == "" {
		args = append(args, "nopass")
	}

	defer func() {
		_ = os.Remove(path.Join(EasyRSAPKI, "reqs", name+".req"))
	}()

	cmd, args := BuildCommand("easyrsa", args...)

	return InteractableExec(
		func(channel int, line string) string {
			if strings.HasPrefix(line, "Enter PEM pass phrase:") || strings.HasPrefix(line, "Verifying - Enter PEM pass phrase:") {
				return pass + "\n"
			}
			if strings.HasPrefix(line, "Enter pass phrase for") {
				return capass + "\n"
			}
			return ""
		},
		cmd,
		args...,
	)
}

func GetClient(name string) (string, error) {
	cmd, args := BuildCommand("ovpn_getclient", name)
	content, err := FlashExec(cmd, args...)
	if err != nil {
		return "", err
	}
	trimmedContent := strings.TrimSpace(content)
	if strings.HasPrefix(trimmedContent, "Unable to find") && strings.HasSuffix(trimmedContent, "please try again or generate the key first") {
		return "", fmt.Errorf("no client named %s", name)
	}
	return content, err
}

func RevokeClient(capass, name string) error {
	cmd, args := BuildCommand("ovpn_revokeclient", name, "remove")

	defer func() {
		// cert
		_ = os.Remove(path.Join(EasyRSAPKI, "issued", name+".crt"))
		// private key
		_ = os.Remove(path.Join(EasyRSAPKI, "private", name+".key"))
		// ccd
		_ = os.Remove(path.Join(OpenVPNConfig, "ccd", name))
	}()

	return InteractableExec(
		func(channel int, line string) string {
			if strings.HasPrefix(line, "Continue with revocation:") {
				return "yes\n"
			} else if strings.HasPrefix(line, "Enter pass phrase for") {
				return capass + "\n"
			}
			return ""
		},
		cmd,
		args...,
	)
}

func Initialize(capass string) error {
	u, err := url.Parse(Hostname)
	if err != nil {
		return err
	}

	var cmd string
	var args []string

	cmd, args = BuildCommand("ovpn_genconfig", "-u", Hostname)
	_, err = FlashExec(cmd, args...)
	if err != nil {
		return err
	}

	cmd, args = BuildCommand("ovpn_initpki")

	return InteractableExec(
		func(channel int, line string) string {
			if strings.HasPrefix(line, "Confirm removal:") {
				return "yes\n"
			} else if strings.HasPrefix(line, "Enter New CA Key Passphrase:") ||
				strings.HasPrefix(line, "Re-Enter New CA Key Passphrase:") ||
				strings.HasPrefix(line, "Enter pass phrase for") {
				return capass + "\n"
			} else if strings.HasPrefix(line, "Common Name (eg: your user, host, or server name) [Easy-RSA CA]:") {
				return u.Hostname() + "\n"
			}
			return ""
		},
		cmd,
		args...,
	)
}
