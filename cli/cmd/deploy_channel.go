package cmd

import (
	"fmt"
	"io"
	"strings"

	"github.com/pkg/errors"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/timoth-y/chainmetric-network/cli/shared"
	"github.com/timoth-y/chainmetric-network/cli/util"
)

// channelCmd represents the channel command
var channelCmd = &cobra.Command{
	Use:   "channel",
	Short: "Performs setup sequence of the Fabric channel for organization",
	RunE: deployChannel,
}

func init() {
	deployCmd.AddCommand(channelCmd)

	channelCmd.Flags().StringP("org", "o", "", "Organization owning peer (required)")
	channelCmd.Flags().StringP("peer", "p", "peer0", "Peer hostname")
	channelCmd.Flags().StringP("channel", "c", "", "Channel name (required)")
}

func deployChannel(cmd *cobra.Command, args []string) error {
	var (
		err           error
		org           string
		peer          string
		channel       string
		channelExists bool
	)

	// Parse flags
	if org, err = cmd.Flags().GetString("org"); err != nil {
		return errors.Wrap(err, "failed to parse required parameter 'org' (organization)")
	} else if len(org) == 0 {
		return errors.New("Required parameter 'org' (organization) is not specified")
	}

	if peer, err = cmd.Flags().GetString("peer"); err != nil {
		return errors.Wrap(err, "failed to parse 'peer' parameter")
	}

	if channel, err = cmd.Flags().GetString("channel"); err != nil {
		return errors.Wrap(err, "failed to parse required 'channel' parameter")
	} else if len(channel) == 0 {
		return errors.New("Required parameter 'channel' is not specified")
	}

	var (
		peerPodName = fmt.Sprintf("%s.%s.org", peer, org)
		cliPodName  = fmt.Sprintf("cli.%s.%s.org", peer, org)
	)

	// Waiting for 'org.peer' pod readiness:
	if ok, err := util.WaitForPodReady(
		cmd.Context(),
		&peerPodName,
		fmt.Sprintf("fabnetd/app=%s.%s.org", peer, org), *namespace,
	); err != nil {
		return err
	} else if !ok {
		return nil
	}

	// Waiting for 'org.peer.cli' pod readiness:
	if ok, err := util.WaitForPodReady(
		cmd.Context(),
		&cliPodName,
		fmt.Sprintf("fabnetd/app=cli.%s.%s.org", peer, org),
		*namespace,
	); err != nil {
		return err
	} else if !ok {
		return nil
	}

	var (
		joinCmd = strings.Join([]string {
			"peer channel join",
			"-b", fmt.Sprintf("%s.block", channel),
		}, " ")

		fetchCmd = strings.Join([]string {
			"peer channel fetch config", fmt.Sprintf("%s.block", channel),
			"-c", channel,
			"-o", fmt.Sprintf("%s.%s:443", viper.GetString("fabric.orderer_hostname_name"), *domain),
			"--tls", "--cafile", "$ORDERER_CA",
		}, " ")

		createCmd = strings.Join([]string {
			"peer channel create",
			"-c", channel,
			"-f", fmt.Sprintf("./channel-artifacts/%s.tx", channel),
			"-o", fmt.Sprintf("%s.%s:443", viper.GetString("fabric.orderer_hostname_name"), *domain),
			"--tls", "--cafile", "$ORDERER_CA",
		}, " ")
	)

	// Checking whether specified channel is already created or not,
	// by trying to fetch in genesis block:
	if _, _, err = util.ExecShellInPod(cmd.Context(), cliPodName, *namespace, fetchCmd); err == nil {
		channelExists = true
		cmd.Println(viper.GetString("cli.info_emoji"),
			fmt.Sprintf("Channel '%s' already created, fetched its genesis block", channel),
		)
	} else if errors.Cause(err) != util.ErrRemoteCmdFailed {
		return errors.Wrapf(err, "Failed to execute command on '%s' pod", cliPodName)
	}

	var stderr io.Reader

	// Creating channel in case it wasn't yet:
	if !channelExists {
		if err = shared.DecorateWithInteractiveLog(func() error {
			if _, stderr, err = util.ExecShellInPod(cmd.Context(), cliPodName, *namespace, createCmd); err != nil {
				if errors.Cause(err) == util.ErrRemoteCmdFailed {
					return errors.Wrapf(err, "Failed to execute command on '%s' pod", cliPodName)
				}

				return errors.New("Failed to create channel")
			}
			return nil
		}, "Creating channel",
			fmt.Sprintf("Channel '%s' successfully created", channel),
		); err != nil {
			return util.WrapWithStderrViewPrompt(err, stderr)
		}
	}

	// Joining peer to channel:
	if err = shared.DecorateWithInteractiveLog(func() error {
		if _, stderr, err = util.ExecShellInPod(cmd.Context(), cliPodName, *namespace, joinCmd); err != nil {
			if errors.Cause(err) == util.ErrRemoteCmdFailed {
				return errors.Wrapf(err, "Failed to execute command on '%s' pod", cliPodName )
			}

			return errors.Wrap(err, "Failed to join channel")
		}
		return nil
	}, fmt.Sprintf("Joinging '%s' organization to '%s' channel", org, channel),
		fmt.Sprintf("Organization '%s' successfully joined '%s' channel", org, channel),
	); err != nil {
		return util.WrapWithStderrViewPrompt(err, stderr)
	}

	cmd.Printf("🎉 Channel '%s' successfully deployed!\n", channel)

	return nil
}
