package mixedoscluster

import (
	"flag"
	"fmt"
	"regexp"
	"strings"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/rancher/rke2/tests/terraform"
	"github.com/rancher/rke2/tests/terraform/createcluster"
)

// var tfVars = flag.String("tfvars", "/tests/terraform/modules/config/local.tfvars", "custom .tfvars file from base project path")
var destroy = flag.Bool("destroy", false, "a bool")

func Test_TFMixedOSClusterCreateValidation(t *testing.T) {
	RegisterFailHandler(Fail)
	flag.Parse()

	RunSpecs(t, "Create Cluster Test Suite")
}

var _ = Describe("Test:", func() {
	Context("Build Mixed OS Cluster:", func() {
		It("Starts up with no issues", func() {
			status, err := createcluster.BuildCluster(&testing.T{}, false)
			Expect(err).NotTo(HaveOccurred())
			Expect(status).To(Equal("cluster created"))
			defer GinkgoRecover()
			fmt.Println("Server Node IPS:", createcluster.MasterIPs)
			fmt.Println("Agent Node IPS:", createcluster.WorkerIPs)
			fmt.Println("Windows Agent Node IPS:", createcluster.WinWorkerIPs)
			terraform.PrintFileContents(createcluster.KubeConfigFile)
			Expect(createcluster.MasterIPs).ShouldNot(BeEmpty())
			if createcluster.NumWorkers > 0 {
				Expect(createcluster.WorkerIPs).ShouldNot(BeEmpty())
			} else {
				Expect(createcluster.WorkerIPs).Should(BeEmpty())
			}
			Expect(createcluster.KubeConfigFile).ShouldNot(BeEmpty())
		})

		It("Checks Node and Pod Status", func() {
			defer func() {
				fmt.Printf("\nFetching node status\n")
				_, err := terraform.Nodes(createcluster.KubeConfigFile, true)
				if err != nil {
					fmt.Println("Error retrieving nodes: ", err)
				}
				fmt.Printf("\nFetching pod status\n")
				_, err = terraform.Pods(createcluster.KubeConfigFile, true)
				if err != nil {
					fmt.Println("Error retrieving pods: ", err)
				}
			}()

			expectedNodeCount := createcluster.NumServers + createcluster.NumWorkers + createcluster.NumWinWorkers
			Eventually(func(g Gomega) {
				nodes, err := terraform.Nodes(createcluster.KubeConfigFile, false)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(len(nodes)).To(Equal(expectedNodeCount), "Number of nodes should match the spec")
				for _, node := range nodes {
					g.Expect(node.Status).Should(Equal("Ready"), "Nodes should all be in Ready state")
				}
			}, "600s", "5s").Should(Succeed())

			re := regexp.MustCompile("[0-9]+")
			Eventually(func(g Gomega) {
				pods, err := terraform.Pods(createcluster.KubeConfigFile, false)
				g.Expect(err).NotTo(HaveOccurred())
				for _, pod := range pods {
					if strings.Contains(pod.Name, "helm-install") {
						g.Expect(pod.Status).Should(Equal("Completed"), pod.Name)
					} else {
						g.Expect(pod.Status).Should(Equal("Running"), pod.Name)
						g.Expect(pod.Restarts).Should(Equal("0"), pod.Name)
						numRunning := re.FindAllString(pod.Ready, 2)
						g.Expect(numRunning[0]).Should(Equal(numRunning[1]), pod.Name, "should have all containers running")
					}
				}
			}, "600s", "5s").Should(Succeed())
		})

		// It("Verifies ClusterIP Service", func() {
		// 	namespace := "auto-clusterip"
		// 	_, err := terraform.DeployWorkload("clusterip.yaml", createcluster.KubeConfigFile)
		// 	Expect(err).NotTo(HaveOccurred(), "Cluster IP manifest not deployed")
		// 	defer terraform.RemoveWorkload("clusterip.yaml", createcluster.KubeConfigFile)

		// 	Eventually(func(g Gomega) {
		// 		cmd := "kubectl get pods -n " + namespace + " -o=name -l k8s-app=nginx-app-clusterip --field-selector=status.phase=Running --kubeconfig=" + createcluster.KubeConfigFile
		// 		res, err := terraform.RunCommand(cmd)
		// 		g.Expect(err).NotTo(HaveOccurred())
		// 		g.Expect(res).Should((ContainSubstring("test-clusterip")))
		// 	}, "420s", "5s").Should(Succeed())

		// 	clusterip, port, _ := terraform.FetchClusterIP(createcluster.KubeConfigFile, namespace, "nginx-clusterip-svc")
		// 	cmd := "curl -sL --insecure http://" + clusterip + ":" + port + "/name.html"
		// 	nodeExternalIP := terraform.FetchNodeExternalIP(createcluster.KubeConfigFile)
		// 	for _, ip := range nodeExternalIP {
		// 		Eventually(func(g Gomega) {
		// 			res, err := terraform.RunCommandOnNode(cmd, ip, createcluster.AwsUser, createcluster.AccessKey)
		// 			g.Expect(err).NotTo(HaveOccurred())
		// 			g.Expect(res).Should(ContainSubstring("test-clusterip"))
		// 		}, "420s", "10s").Should(Succeed())
		// 	}
		// })

		It("Verifies internode connectivity over the vxlan tunnel", func() {
			_, err := terraform.DeployWorkload("pod_client.yaml", createcluster.KubeConfigFile)
			Expect(err).NotTo(HaveOccurred(), "pod_client manifest not deployed")
			_, err = terraform.DeployWorkload("windows_app_deployment.yaml", createcluster.KubeConfigFile)
			Expect(err).NotTo(HaveOccurred(), "windows_app_deployment manifest not deployed")
			defer terraform.RemoveWorkload("pod_client.yaml", createcluster.KubeConfigFile)
			defer terraform.RemoveWorkload("windows_app_deployment.yaml", createcluster.KubeConfigFile)
			
			// Wait for the pod_client pods to have an IP
			Eventually(func() string {
				cmd := `kubectl get pods -l app=client -o=jsonpath='{range .items[*]}{.status.podIPs[*].ip}{" "}{end}' --kubeconfig=` + createcluster.KubeConfigFile
				res, _ := terraform.RunCommand(cmd)//, createcluster.MasterIPs, createcluster.AwsUser, createcluster.AccessKey)
				ips :=  strings.Split(res, " ") //e2e.PodIPsUsingLabel(kubeConfigFile, "app=client")
				return ips[0]
			}, "120s", "10s").Should(ContainSubstring("10.42"), "failed getClientIPs")
	
			// Wait for the windows_app_deployment pods to have an IP (We must wait 250s because it takes time)
			Eventually(func() string {
				cmd := `kubectl get pods -l app=windows-app -o=jsonpath='{range .items[*]}{.status.podIPs[*].ip}{" "}{end}' --kubeconfig=` + createcluster.KubeConfigFile
				res, _ :=  terraform.RunCommand(cmd)
				ips :=  strings.Split(res, " ")
				return ips[0]
			}, "120s", "10s").Should(ContainSubstring("10.42"), "failed getWinAppClientIPs")
	
			// Test Linux -> Windows communication
			cmd := "kubectl exec svc/client-curl --kubeconfig=" + createcluster.KubeConfigFile + " -- curl -m7 windows-app-svc:3000"
			Eventually(func() (string, error) {
				return terraform.RunCommand(cmd)
			}, "120s", "3s").Should(ContainSubstring("Welcome to PSTools for K8s Debugging"), "failed cmd: "+cmd)
	
			// Test Windows -> Linux communication
			cmd = "kubectl exec svc/windows-app-svc --kubeconfig=" + createcluster.KubeConfigFile + " -- curl -m7 client-curl:8080"
			Eventually(func() (string, error) {
				return terraform.RunCommand(cmd)
			}, "120s", "3s").Should(ContainSubstring("Welcome to nginx!"), "failed cmd: "+cmd)
		})

		It("Runs the mixed os sonobuoy plugin", func() {
			scriptsDir := terraform.Basepath() + `/tests/terraform/scripts`
			fmt.Println(scriptsDir)
			cmd := `chmod +x ` + scriptsDir + `/install_sonobuoy.sh && sh ` + scriptsDir + `/install_sonobuoy.sh`
			fmt.Println(cmd)
			res, err := terraform.RunCommand(cmd)
			Expect(err).NotTo(HaveOccurred(), "failed cmd: " + res)
			cmd = `sonobuoy run --kubeconfig=` + createcluster.KubeConfigFile + ` --plugin my-sonobuoy-plugins/mixed-workload-e2e/mixed-workload-e2e.yaml --aggregator-node-selector kubernetes.io/os:linux --wait`
			fmt.Println(cmd)
			res, err = terraform.RunCommand(cmd)
			Expect(err).NotTo(HaveOccurred(), "failed output: " + res)
			cmd = `sonobuoy retrieve --kubeconfig=`+ createcluster.KubeConfigFile
			testResultTar, err := terraform.RunCommand(cmd)
			Expect(err).NotTo(HaveOccurred(), "failed cmd: "+ cmd)
			cmd = "sonobuoy results " + testResultTar
			res, err = terraform.RunCommand(cmd)
			Expect(err).NotTo(HaveOccurred(), "failed cmd: "+ cmd)
			Expect(res).Should(ContainSubstring("Plugin: mixed-workload-e2e\nStatus: passed\n"))
		})
	})
})

var _ = BeforeEach(func() {
	if *destroy {
		Skip("Cluster is being Deleted")
	}
})

var _ = AfterEach(func() {
	if CurrentSpecReport().Failed() {
		fmt.Printf("\nFAILED! %s\n", CurrentSpecReport().FullText())
	} else {
		fmt.Printf("\nPASSED! %s\n", CurrentSpecReport().FullText())
	}
})

var _ = AfterSuite(func() {
	if *destroy {
		status, err := createcluster.BuildCluster(&testing.T{}, *destroy)
		Expect(err).NotTo(HaveOccurred())
		Expect(status).To(Equal("cluster destroyed"))
	}
})
