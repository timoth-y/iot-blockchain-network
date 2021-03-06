package cmd

import (
	"context"
	"fmt"
	"io/ioutil"
	"path"

	"github.com/mittwald/go-helm-client"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/timoth-y/chainmetric-network/cli/shared"
	"github.com/timoth-y/chainmetric-network/cli/util"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/yaml"
)

// ordererCmd represents the orderer command.
var ordererCmd = &cobra.Command{
	Use:   "orderer",
	Short: "Performs deployment sequence of the Fabric orderer service",
	Long: `Performs deployment sequence of the Fabric orderer service

Examples:
  # Deploy orderer:
  fabnctl deploy orderer -d example.com`,

	RunE: handleErrors(deployOrderer),
}

func init() {
	deployCmd.AddCommand(ordererCmd)
}

func deployOrderer(cmd *cobra.Command, _ []string) error {
	var (
		hostname = viper.GetString("fabric.orderer_hostname_name")
		tlsDir   = path.Join(
			fmt.Sprintf(".crypto-config.%s", domain),
			"ordererOrganizations", domain,
			"orderers", fmt.Sprintf("%s.%s", hostname, domain),
			"tls",
		)
		pkPath        = path.Join(tlsDir, "server.key")
		certPath      = path.Join(tlsDir, "server.crt")
		caPath        = path.Join(tlsDir, "ca.crt")
		tlsSecretName = fmt.Sprintf("%s.%s-tls", hostname, domain)
		caSecretName  = fmt.Sprintf("%s.%s-ca", hostname, domain)
	)

	// Retrieve orderer transport TLS private key:
	pkPayload, err := ioutil.ReadFile(pkPath)
	if err != nil {
		return errors.Wrapf(err, "failed to read private key from path: %s", pkPath)
	}

	// Retrieve orderer transport TLS cert:
	certPayload, err := ioutil.ReadFile(certPath)
	if err != nil {
		return errors.Wrapf(err, "failed to read certificate identity from path: %s", certPath)
	}

	// Retrieve orderer transport CA cert:
	caPayload, err := ioutil.ReadFile(caPath)
	if err != nil {
		return errors.Wrapf(err, "failed to read certificate CA from path: %s", caPath)
	}

	// Create or update orderer transport TLS secret:
	if _, err = util.SecretAdapter(shared.K8s.CoreV1().Secrets(namespace)).CreateOrUpdate(cmd.Context(), corev1.Secret{
		Type: corev1.SecretTypeTLS,
		Data: map[string][]byte{
			corev1.TLSPrivateKeyKey: pkPayload,
			corev1.TLSCertKey:       certPayload,
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      tlsSecretName,
			Namespace: namespace,
			Labels: map[string]string{
				"fabnctl/cid": "orderer.tls.secret",
				"fabnctl/domain": domain,
				"fabnctl/host": hostname,
			},
		},
	}); err != nil {
		return errors.Wrapf(err, "failed to create %s secret", tlsSecretName)
	}

	cmd.Printf("%s Secret '%s' successfully created\n",
		viper.GetString("cli.success_emoji"), tlsSecretName,
	)

	// Create or update orderer transport CA secret:
	if _, err = util.SecretAdapter(shared.K8s.CoreV1().Secrets(namespace)).CreateOrUpdate(cmd.Context(), corev1.Secret{
		Type: corev1.SecretTypeOpaque,
		Data: map[string][]byte{
			"ca.crt": caPayload,
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: caSecretName,
			Namespace: namespace,
			Labels: map[string]string{
				"fabnctl/cid": "orderer.ca.secret",
				"fabnctl/domain": domain,
				"fabnctl/host": hostname,
			},
		},
	}); err != nil {
		return errors.Wrapf(err, "failed to create %s secret", caSecretName)
	}

	cmd.Printf("%s Secret '%s' successfully created\n",
		viper.GetString("cli.success_emoji"),
		caSecretName,
	)

	// Preparing additional values for chart installation:
	var (
		values = make(map[string]interface{})
		chartSpec = &helmclient.ChartSpec{
			ReleaseName: "orderer",
			ChartName: path.Join(chartsPath, "orderer"),
			Namespace: namespace,
			Wait: true,
		}
	)

	if targetArch == "arm64" {
		armValues, err := util.ValuesFromFile(path.Join(chartsPath, "orderer", "values.arm64.yaml"))
		if err != nil {
			return err
		}
		values = armValues
	}

	values["domain"] = domain

	valuesYaml, err := yaml.Marshal(values)
	if err != nil {
		return errors.Wrap(err, "failed to encode additional values")
	}

	chartSpec.ValuesYaml = string(valuesYaml)

	// Installing orderer helm chart:
	ctx, cancel := context.WithTimeout(cmd.Context(), viper.GetDuration("helm.install_timeout"))
	defer cancel()

	if err = shared.DecorateWithInteractiveLog(func() error {
		if err = shared.Helm.InstallOrUpgradeChart(ctx, chartSpec); err != nil {
			return errors.Wrap(err, "failed to install orderer helm chart")
		}
		return nil
	}, "Installing orderer chart", "Chart 'orderer/orderer' installed successfully"); err != nil {
		return nil
	}

	cmd.Printf("🎉 Orderer service successfully deployed on %s.%s!\n", hostname, domain)
	return nil
}
