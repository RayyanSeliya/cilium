// SPDX-License-Identifier: Apache-2.0
// Copyright Authors of Cilium

package helpers

import (
	"bytes"
	"context"
	"fmt"
	"html/template"
	"io"
	"math/rand/v2"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"golang.org/x/sys/unix"

	"github.com/cilium/cilium/test/config"
	ginkgoext "github.com/cilium/cilium/test/ginkgo-ext"
)

// Sleep sleeps for the specified duration in seconds
func Sleep(delay time.Duration) {
	time.Sleep(delay * time.Second)
}

// MakeUID returns a randomly generated string.
func MakeUID() string {
	return fmt.Sprintf("%08x", rand.Uint32())
}

// RenderTemplate renders a text/template string into a buffer.
// Returns eturn an error if the template cannot be validated.
func RenderTemplate(tmplt string) (*bytes.Buffer, error) {
	t, err := template.New("").Parse(tmplt)
	if err != nil {
		return nil, err
	}
	content := new(bytes.Buffer)
	err = t.Execute(content, nil)
	if err != nil {
		return nil, err
	}
	return content, nil
}

// TimeoutConfig represents the configuration for the timeout of a command.
type TimeoutConfig struct {
	Ticker  time.Duration // Check interval
	Timeout time.Duration // Limit for how long to spend in the command
}

// Validate ensuires that the parameters for the TimeoutConfig are reasonable
// for running in tests.
func (c *TimeoutConfig) Validate() error {
	if c.Timeout < 5*time.Second {
		return fmt.Errorf("Timeout too short (must be at least 5 seconds): %v", c.Timeout)
	}
	if c.Ticker == 0 {
		c.Ticker = 1 * time.Second
	} else if c.Ticker < time.Second {
		return fmt.Errorf("Timeout config Ticker interval too short (must be at least 1 second): %v", c.Ticker)
	}
	return nil
}

// WithTimeout executes body using the time interval specified in config until
// the timeout in config is reached. Returns an error if the timeout is
// exceeded for body to execute successfully.
func WithTimeout(body func() bool, msg string, config *TimeoutConfig) error {
	err := RepeatUntilTrue(body, config)
	if err != nil {
		return fmt.Errorf("%s: %w", msg, err)
	}

	return nil
}

// RepeatUntilTrueDefaultTimeout calls RepeatUntilTrue with the default timeout
// HelperTimeout
func RepeatUntilTrueDefaultTimeout(body func() bool) error {
	return RepeatUntilTrue(body, &TimeoutConfig{Timeout: HelperTimeout})
}

// RepeatUntilTrue repeatedly calls body until body returns true or the timeout
// expires
func RepeatUntilTrue(body func() bool, config *TimeoutConfig) error {
	if err := config.Validate(); err != nil {
		return err
	}

	bodyChan := make(chan bool, 1)

	asyncBody := func(ch chan bool) {
		defer ginkgo.GinkgoRecover()
		success := body()
		ch <- success
		if success {
			close(ch)
		}
	}

	go asyncBody(bodyChan)

	done := time.After(config.Timeout)
	ticker := time.NewTicker(config.Ticker)
	defer ticker.Stop()
	for {
		select {
		case success := <-bodyChan:
			if success {
				return nil
			}
			// Provide some form of rate-limiting here before running next
			// execution in case body() returns at a fast rate.
			<-ticker.C
			go asyncBody(bodyChan)
		case <-done:
			return fmt.Errorf("%s timeout expired", config.Timeout)
		}
	}
}

// WithContext executes body with the given frequency. The function
// f is executed until bool returns true or the given context signalizes Done.
// `f` should stop if context is canceled.
func WithContext(ctx context.Context, f func(ctx context.Context) (bool, error), freq time.Duration) error {
	ticker := time.NewTicker(freq)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			stop, err := f(ctx)
			if err != nil {
				select {
				case <-ctx.Done():
					return ctx.Err()
				default:
					return err
				}
			}
			if stop {
				select {
				case <-ctx.Done():
					return ctx.Err()
				default:
					return nil
				}
			}
		}
	}
}

// GetAppPods fetches app pod names for a namespace.
// For Http based tests, we identify pods with format id=<pod_name>, while
// for Kafka based tests, we identify pods with the format app=<pod_name>.
func GetAppPods(apps []string, namespace string, kubectl *Kubectl, appFmt string) map[string]string {
	appPods := make(map[string]string)
	for _, v := range apps {
		res, err := kubectl.GetPodNames(namespace, fmt.Sprintf("%s=%s", appFmt, v))
		Expect(err).Should(BeNil())
		Expect(res).Should(Not(BeNil()))
		Expect(len(res)).To(BeNumerically(">", 0))
		appPods[v] = res[0]
		log.Infof("GetAppPods: pod=%q assigned to %q", res[0], v)
	}
	return appPods
}

// HoldEnvironment prints the current test status, then pauses the test
// execution. Developers who are writing tests may wish to invoke this function
// directly from test code to assist troubleshooting and test development.
func HoldEnvironment(description ...string) {
	test := ginkgo.CurrentGinkgoTestDescription()
	pid := unix.Getpid()

	fmt.Fprintf(os.Stdout, "\n---\n%s", test.FullTestText)
	fmt.Fprintf(os.Stdout, "\nat %s:%d", test.FileName, test.LineNumber)
	fmt.Fprintf(os.Stdout, "\n\n%s", description)
	fmt.Fprintf(os.Stdout, "\n\nPausing test for debug.")
	fmt.Fprintf(os.Stdout, "\nRun \"kill -SIGCONT %d\" to continue.\n", pid)
	unix.Kill(pid, unix.SIGSTOP)
	time.Sleep(time.Millisecond)
	fmt.Fprintf(os.Stdout, "Test resumed.\n")
}

// Fail is a Ginkgo failure handler which raises a SIGSTOP for the test process
// when there is a failure, so that developers can debug the live environment.
// It is only triggered if the developer provides a commandline flag.
func Fail(description string, callerSkip ...int) {
	if len(callerSkip) > 0 {
		callerSkip[0]++
	} else {
		callerSkip = []int{1}
	}

	if config.CiliumTestConfig.HoldEnvironment {
		HoldEnvironment(description)
	}
	ginkgoext.Fail(description, callerSkip...)
}

// ReportDirectoryPath determines the directory path for exporting report
// commands in the case of test failure.
func ReportDirectoryPath() string {
	prefix := ""
	testName := ginkgoext.GetTestName()
	if strings.HasPrefix(strings.ToLower(testName), K8s) {
		prefix = fmt.Sprintf("%s-", strings.ReplaceAll(GetCurrentK8SEnv(), ".", ""))
	}
	return filepath.Join(TestResultsPath, prefix, testName)
}

// CreateReportDirectory creates and returns the directory path to export all report
// commands that need to be run in the case that a test has failed.
// If the directory cannot be created it'll return an error
func CreateReportDirectory() (string, error) {
	testPath := ReportDirectoryPath()
	if _, err := os.Stat(testPath); err == nil {
		return testPath, nil
	}
	err := os.MkdirAll(testPath, os.ModePerm)
	return testPath, err
}

// CreateLogFile creates the ReportDirectory if it is not present, writes the
// given data to the given filename.
func CreateLogFile(filename string, data []byte) error {
	path, err := CreateReportDirectory()
	if err != nil {
		log.WithError(err).Errorf("ReportDirectory cannot be created")
		return err
	}

	finalPath := filepath.Join(path, filename)
	return os.WriteFile(finalPath, data, LogPerm)
}

// WriteToReportFile writes data to filename. It appends to existing files.
func WriteToReportFile(data []byte, filename string) error {
	testPath, err := CreateReportDirectory()
	if err != nil {
		log.WithError(err).Errorf("cannot create test results path '%s'", testPath)
		return err
	}

	err = WriteOrAppendToFile(
		filepath.Join(testPath, filename),
		data,
		LogPerm)
	if err != nil {
		log.WithError(err).Errorf("cannot create monitor log file %s", filename)
		return err
	}
	return nil
}

// reportMap saves the output of the given commands to the specified filename.
// Function needs a directory path where the files are going to be written and
// a *SSHMeta instance to execute the commands
func reportMap(path string, reportCmds map[string]string, node *SSHMeta) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	reportMapContext(ctx, path, reportCmds, node)
}

// reportMap saves the output of the given commands to the specified filename.
// Function needs a directory path where the files are going to be written and
// a *SSHMeta instance to execute the commands
func reportMapContext(ctx context.Context, path string, reportCmds map[string]string, node *SSHMeta) {
	if node == nil {
		log.Errorf("cannot execute reportMap due invalid node instance")
		return
	}

	for cmd, logfile := range reportCmds {
		res := node.ExecContext(ctx, cmd, ExecOptions{SkipLog: true})
		err := os.WriteFile(
			fmt.Sprintf("%s/%s", path, logfile),
			res.CombineOutput().Bytes(),
			LogPerm)
		if err != nil {
			log.WithError(err).Errorf("cannot create test results for command '%s'", cmd)
		}
	}
}

// ManifestGet returns the full path of the given manifest corresponding to the
// Kubernetes version being tested, if such a manifest exists, if not it
// returns the global manifest file.
// The paths are checked in order:
// 1- base_path/integration/filename
// 2- base_path/k8s_version/integration/filename
// 3- base_path/k8s_version/filename
// 4- base_path/filename
func ManifestGet(base, manifestFilename string) string {
	// Try dependent integration file only if we have one configured. This is
	// needed since no integration is "" and that causes us to find the
	// base_path/filename before we check the base_path/k8s_version/filename
	if integration := GetCurrentIntegration(); integration != "" {
		fullPath := filepath.Join(K8sManifestBase, integration, manifestFilename)
		_, err := os.Stat(fullPath)
		if err == nil {
			return filepath.Join(base, fullPath)
		}

		// try dependent k8s version and integration file
		fullPath = filepath.Join(K8sManifestBase, GetCurrentK8SEnv(), integration, manifestFilename)
		_, err = os.Stat(fullPath)
		if err == nil {
			return filepath.Join(base, fullPath)
		}
	}

	// try dependent k8s version
	fullPath := filepath.Join(K8sManifestBase, GetCurrentK8SEnv(), manifestFilename)
	_, err := os.Stat(fullPath)
	if err == nil {
		return filepath.Join(base, fullPath)
	}
	return filepath.Join(base, K8sManifestBase, manifestFilename)
}

// WriteOrAppendToFile writes data to a file named by filename.
// If the file does not exist, WriteFile creates it with permissions perm;
// otherwise WriteFile appends the data to the file
func WriteOrAppendToFile(filename string, data []byte, perm os.FileMode) error {
	f, err := os.OpenFile(filename, os.O_APPEND|os.O_WRONLY|os.O_CREATE, perm)
	if err != nil {
		return err
	}
	n, err := f.Write(data)
	if err == nil && n < len(data) {
		err = io.ErrShortWrite
	}
	if err1 := f.Close(); err == nil {
		err = err1
	}
	return err
}

// failIfContainsBadLogMsg makes a test case to fail if any message from
// given log messages contains an entry from the blacklist (map key) AND
// does not contain ignore messages (map value).
func failIfContainsBadLogMsg(logs, label string, blacklist map[string][]string) {
	uniqueFailures := make(map[string]int)
	for msg := range strings.SplitSeq(logs, "\n") {
		for fail, ignoreMessages := range blacklist {
			if strings.Contains(msg, fail) {
				ok := false
				for _, ignore := range ignoreMessages {
					if strings.Contains(msg, ignore) {
						ok = true
						break
					}
				}
				if !ok {
					count := uniqueFailures[msg]
					uniqueFailures[msg] = count + 1
				}
			}
		}
	}
	if len(uniqueFailures) > 0 {
		failures := make([]string, 0, len(uniqueFailures))
		for f, c := range uniqueFailures {
			failures = append(failures, f)
			fmt.Fprintf(CheckLogs, "⚠️  Found %q in logs %d times\n", f, c)
		}
		failureMsgs := strings.Join(failures, "\n")
		Fail(fmt.Sprintf("Found %d %s logs matching list of errors that must be investigated:\n%s", len(uniqueFailures), label, failureMsgs))
	}
}

// RunsOnNetNextKernel checks whether a test case is running on the net-next
// kernel (depending on the image, it's the latest kernel either from net-next.git
// or bpf-next.git tree).
func RunsOnNetNextKernel() bool {
	netNext := os.Getenv("NETNEXT")
	if netNext == "true" || netNext == "1" {
		return true
	}
	netNext = os.Getenv("KERNEL")
	return netNext == "net-next"
}

// DoesNotRunOnNetNextKernel is the complement function of RunsOnNetNextKernel.
func DoesNotRunOnNetNextKernel() bool {
	return !RunsOnNetNextKernel()
}

// RunsOn54Kernel checks whether a test case is running on the 5.4 kernel.
func RunsOn54Kernel() bool {
	return os.Getenv("KERNEL") == "54"
}

// DoesNotRunOn54Kernel is the complement function of RunsOn54Kernel.
func DoesNotRunOn54Kernel() bool {
	return !RunsOn54Kernel()
}

func NativeRoutingCIDR() string {
	return os.Getenv("NATIVE_CIDR")
}

// RunsOn54OrLaterKernel checks whether a test case is running on 5.4 or later kernel
func RunsOn54OrLaterKernel() bool {
	return RunsOnNetNextKernel() || RunsOn54Kernel()
}

// DoesNotRunOn54OrLaterKernel is the complement function of RunsOn54OrLaterKernel
func DoesNotRunOn54OrLaterKernel() bool {
	return !RunsOn54OrLaterKernel()
}

// RunsOnGKE returns true if the tests are running on GKE.
func RunsOnGKE() bool {
	return GetCurrentIntegration() == CIIntegrationGKE
}

// DoesNotRunOnGKE is the complement function of DoesNotRunOnGKE.
func DoesNotRunOnGKE() bool {
	return !RunsOnGKE()
}

// RunsOnAKS returns true if the tests are running on AKS.
func RunsOnAKS() bool {
	return GetCurrentIntegration() == CIIntegrationAKS
}

// DoesNotRunOnAKS is the complement function of DoesNotRunOnAKS.
func DoesNotRunOnAKS() bool {
	return !RunsOnAKS()
}

// RunsWithKubeProxyReplacement returns true if the kernel supports our
// kube-proxy replacement. Note that kube-proxy may still be running
// alongside Cilium.
func RunsWithKubeProxyReplacement() bool {
	return RunsOnGKE() || RunsOn54OrLaterKernel()
}

// DoesNotRunWithKubeProxyReplacement is the complement function of
// RunsWithKubeProxyReplacement.
func DoesNotRunWithKubeProxyReplacement() bool {
	return !RunsWithKubeProxyReplacement()
}

// RunsWithHostFirewall returns true is Cilium runs with the host firewall enabled.
func RunsWithHostFirewall() bool {
	return os.Getenv("HOST_FIREWALL") != "0" && os.Getenv("HOST_FIREWALL") != ""
}

// RunsWithKubeProxy returns true if cilium runs together with k8s' kube-proxy.
func RunsWithKubeProxy() bool {
	return os.Getenv("KUBEPROXY") != "0"
}

// ExistNodeWithoutCilium returns true if there is a node in a cluster which does
// not run Cilium.
func ExistNodeWithoutCilium() bool {
	return len(GetNodesWithoutCilium()) > 0
}

// DoesNotExistNodeWithoutCilium is the complement function of ExistNodeWithoutCilium.
func DoesNotExistNodeWithoutCilium() bool {
	return !ExistNodeWithoutCilium()
}

// HasSocketLB returns true if the given Cilium pod has TCP and/or
// UDP host reachable services are enabled.
func (kub *Kubectl) HasSocketLB(pod string) bool {
	status := kub.CiliumExecContext(context.TODO(), pod,
		"cilium-dbg status -o jsonpath='{.kube-proxy-replacement.features.socketLB}'")
	status.ExpectSuccess("Failed to get status: %s", status.OutputPrettyPrint())
	lines := status.ByLines()
	Expect(len(lines)).ShouldNot(Equal(0), "Failed to get socketLB status")

	return strings.Contains(lines[0], "true")
}

// HasBPFNodePort returns true if the given Cilium pod has BPF NodePort enabled.
func (kub *Kubectl) HasBPFNodePort(pod string) bool {
	status := kub.CiliumExecContext(context.TODO(), pod,
		"cilium-dbg status -o jsonpath='{.kube-proxy-replacement.features.nodePort.enabled}'")
	status.ExpectSuccess("Failed to get status: %s", status.OutputPrettyPrint())
	lines := status.ByLines()
	Expect(len(lines)).ShouldNot(Equal(0), "Failed to get nodePort status")
	return strings.Contains(lines[0], "true")
}

// GetNodesWithoutCilium returns a slice of names for nodes that do not run
// Cilium.
func GetNodesWithoutCilium() []string {
	if os.Getenv("NO_CILIUM_ON_NODES") == "" {
		if os.Getenv("NO_CILIUM_ON_NODE") == "" {
			return []string{}
		}
		return []string{os.Getenv("NO_CILIUM_ON_NODE")}
	}
	return strings.Split(os.Getenv("NO_CILIUM_ON_NODES"), ",")
}

// GetFirstNodeWithoutCilium returns the first node that does not run Cilium.
// It's the responsibility of the caller to check that there are nodes without
// Cilium.
func GetFirstNodeWithoutCilium() string {
	noCiliumNodes := GetNodesWithoutCilium()
	return noCiliumNodes[0]
}

// GetFirstNodeWithoutCiliumLabel returns the ci-node label value of the first node
// which is running without Cilium.
func (kub *Kubectl) GetFirstNodeWithoutCiliumLabel() string {
	nodeName := GetFirstNodeWithoutCilium()
	return kub.GetNodeCILabel(nodeName)
}

// GetNodeCILabel returns the ci-node label value of the given node.
func (kub *Kubectl) GetNodeCILabel(nodeName string) string {
	cmd := fmt.Sprintf("%s get node %s -o jsonpath='{.metadata.labels.cilium\\.io/ci-node}'",
		KubectlCmd, nodeName)
	res := kub.ExecShort(cmd)
	if !res.WasSuccessful() {
		return ""
	}
	return res.SingleOut()
}

// IsNodeWithoutCilium returns true if node node doesn't run Cilium.
func IsNodeWithoutCilium(node string) bool {
	return slices.Contains(GetNodesWithoutCilium(), node)
}

// SkipRaceDetectorEnabled returns whether tests failing with race detector
// enabled should be skipped.
func SkipRaceDetectorEnabled() bool {
	race := os.Getenv("RACE")
	return race == "1" || race == "true"
}

// DualStackSupported returns whether the current environment has DualStack IPv6
// enabled or not for the cluster.
func DualStackSupported() bool {
	// AKS does not support dual stack yet
	if IsIntegration(CIIntegrationAKS) {
		return false
	}

	// We only have DualStack enabled in KIND.
	return GetCurrentIntegration() == "" || IsIntegration(CIIntegrationKind)
}

// DualStackSupportBeta returns true if the environment has a Kubernetes version that
// has support for k8s DualStack beta API types.
func DualStackSupportBeta() bool {
	// AKS does not support dual stack yet
	if IsIntegration(CIIntegrationAKS) {
		return false
	}

	return GetCurrentIntegration() == "" || IsIntegration(CIIntegrationKind)
}

// CiliumEndpointSliceFeatureEnabled returns true only if the environment has a kubernetes version
// greater than or equal to 1.21.
func CiliumEndpointSliceFeatureEnabled() bool {
	return GetCurrentIntegration() == "" || IsIntegration(CIIntegrationKind)
}

// SupportIPv6ToOutside returns true if the CI environment supports IPv6
// connectivity to the outside world.
func SupportIPv6ToOutside() bool {
	return os.Getenv("CILIUM_NO_IPV6_OUTSIDE") == ""
}
