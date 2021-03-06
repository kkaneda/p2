package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/user"

	"gopkg.in/alecthomas/kingpin.v2"

	"github.com/Sirupsen/logrus"
	"github.com/square/p2/pkg/labels"
	"github.com/square/p2/pkg/logging"
	"github.com/square/p2/pkg/pc/control"
	"github.com/square/p2/pkg/pc/fields"
	"github.com/square/p2/pkg/store/consul"
	"github.com/square/p2/pkg/store/consul/flags"
	"github.com/square/p2/pkg/store/consul/pcstore"
	"github.com/square/p2/pkg/types"
	klabels "k8s.io/kubernetes/pkg/labels"
)

const (
	cmdCreateText = "create"
	cmdGetText    = "get"
	cmdDeleteText = "delete"
	cmdUpdateText = "update"
	cmdListText   = "list"
)

var (
	cmdCreate         = kingpin.Command(cmdCreateText, "Create a pod cluster. ")
	createPodID       = cmdCreate.Flag("pod", "The pod ID on the pod cluster").Required().String()
	createAZ          = cmdCreate.Flag("az", "The availability zone of the pod cluster").Required().String()
	createName        = cmdCreate.Flag("name", "The cluster name (ie. staging, production)").Required().String()
	createAnnotations = cmdCreate.Flag("annotations", "Complete set of annotations - must parse as JSON!").String()

	cmdGet   = kingpin.Command(cmdGetText, "Show a pod cluster. ")
	getPodID = cmdGet.Flag("pod", "The pod ID on the pod cluster").String()
	getAZ    = cmdGet.Flag("az", "The availability zone of the pod cluster").String()
	getName  = cmdGet.Flag("name", "The cluster name (ie. staging, production)").String()
	getID    = cmdGet.Flag("id", "The cluster UUID. This option is mutually exclusive with pod,az,name").String()

	cmdDelete   = kingpin.Command(cmdDeleteText, "Delete a pod cluster. ")
	deletePodID = cmdDelete.Flag("pod", "The pod ID on the pod cluster").String()
	deleteAZ    = cmdDelete.Flag("az", "The availability zone of the pod cluster").String()
	deleteName  = cmdDelete.Flag("name", "The cluster name (ie. staging, production)").String()
	deleteID    = cmdDelete.Flag("id", "The cluster UUID. This option is mutually exclusive with pod,az,name").String()

	cmdUpdate         = kingpin.Command(cmdUpdateText, "Update a pod cluster. ")
	updatePodID       = cmdUpdate.Flag("pod", "The pod ID on the pod cluster").String()
	updateAZ          = cmdUpdate.Flag("az", "The availability zone of the pod cluster").String()
	updateName        = cmdUpdate.Flag("name", "The cluster name (ie. staging, production)").String()
	updateAnnotations = cmdUpdate.Flag("annotations", "JSON string representing the complete update ").String()
	updateID          = cmdUpdate.Flag("id", "The cluster UUID. This option is mutually exclusive with pod,az,name").String()

	cmdList = kingpin.Command(cmdListText, "Lists pod clusters. ")
)

func main() {
	cmd, consulOpts, labeler := flags.ParseWithConsulOptions()
	client := consul.NewConsulClient(consulOpts)
	kv := consul.NewConsulStore(client)
	logger := logging.NewLogger(logrus.Fields{})
	pcstore := pcstore.NewConsul(client, labeler, labels.NewConsulApplicator(client, 0), &logger)
	session, _, err := kv.NewSession(fmt.Sprintf("pcctl-%s", currentUserName()), nil)
	if err != nil {
		log.Fatalf("Could not create session: %s", err)
	}

	switch cmd {
	case cmdCreateText:
		az := fields.AvailabilityZone(*createAZ)
		cn := fields.ClusterName(*createName)
		podID := types.PodID(*createPodID)
		selector := selectorFrom(az, cn, podID)
		pccontrol := control.NewPodCluster(az, cn, podID, pcstore, selector, session)

		annotations := *createAnnotations
		var parsedAnnotations map[string]interface{}
		err := json.Unmarshal([]byte(annotations), &parsedAnnotations)
		if err != nil {
			log.Fatalf("could not parse json: %v", err)
		}
		_, err = pccontrol.Create(parsedAnnotations)
		if err != nil {
			log.Fatalf("err: %v", err)
		}
	case cmdGetText:
		az := fields.AvailabilityZone(*getAZ)
		cn := fields.ClusterName(*getName)
		podID := types.PodID(*getPodID)
		pcID := fields.ID(*getID)

		var pccontrol *control.PodCluster
		if pcID != "" {
			pccontrol = control.NewPodClusterFromID(pcID, session, pcstore)
		} else if az != "" && cn != "" && podID != "" {
			selector := selectorFrom(az, cn, podID)
			pccontrol = control.NewPodCluster(az, cn, podID, pcstore, selector, session)
		} else {
			log.Fatalf("Expected one of: pcID or (pod,az,name)")
		}

		pc, err := pccontrol.Get()
		if err != nil {
			log.Fatalf("Caught error while fetching pod cluster: %v", err)
		}

		bytes, err := json.Marshal(pc)
		if err != nil {
			logger.WithError(err).Fatalln("Unable to marshal PC as JSON")
		}
		fmt.Printf("%s", bytes)
	case cmdDeleteText:
		az := fields.AvailabilityZone(*deleteAZ)
		cn := fields.ClusterName(*deleteName)
		podID := types.PodID(*deletePodID)
		pcID := fields.ID(*deleteID)

		var pccontrol *control.PodCluster
		if pcID != "" {
			pccontrol = control.NewPodClusterFromID(pcID, session, pcstore)
		} else if az != "" && cn != "" && podID != "" {
			selector := selectorFrom(az, cn, podID)
			pccontrol = control.NewPodCluster(az, cn, podID, pcstore, selector, session)
		} else {
			log.Fatalf("Expected one of: pcID or (pod,az,name)")
		}

		errors := pccontrol.Delete()
		if len(errors) >= 1 {
			for _, err := range errors {
				_, _ = os.Stderr.Write([]byte(fmt.Sprintf("Failed to delete one pod cluster matching arguments. Error:\n %s\n", err.Error())))
			}
			os.Exit(1)
		}
	case cmdUpdateText:
		az := fields.AvailabilityZone(*updateAZ)
		cn := fields.ClusterName(*updateName)
		podID := types.PodID(*updatePodID)
		pcID := fields.ID(*updateID)

		var pccontrol *control.PodCluster
		if pcID != "" {
			pccontrol = control.NewPodClusterFromID(pcID, session, pcstore)
		} else if az != "" && cn != "" && podID != "" {
			selector := selectorFrom(az, cn, podID)
			pccontrol = control.NewPodCluster(az, cn, podID, pcstore, selector, session)
		} else {
			log.Fatalf("Expected one of: pcID or (pod,az,name)")
		}

		var annotations fields.Annotations
		err := json.Unmarshal([]byte(*updateAnnotations), &annotations)
		if err != nil {
			_, _ = os.Stderr.Write([]byte(fmt.Sprintf("Annotations are invalid JSON. Err follows:\n%v", err)))
			os.Exit(1)
		}

		pc, err := pccontrol.Update(annotations)
		if err != nil {
			log.Fatalf("Error during PodCluster update: %v\n%v", err, pc)
			os.Exit(1)
		}
		bytes, err := json.Marshal(pc)
		if err != nil {
			log.Fatalf("Update succeeded, but error during displaying PC: %v\n%+v", err, pc)
			os.Exit(1)
		}
		fmt.Printf("%s", bytes)
	case cmdListText:
		pcs, err := pcstore.List()
		if err != nil {
			_, _ = os.Stderr.Write([]byte(fmt.Sprintf("Could not list pcs. Err follows:\n%v", err)))
			os.Exit(1)
		}

		bytes, err := json.Marshal(pcs)
		if err != nil {
			_, _ = os.Stderr.Write([]byte(fmt.Sprintf("Could not marshal pc list. Err follows:\n%v", err)))
			os.Exit(1)
		}
		fmt.Printf("%s", bytes)
	default:
		log.Fatalf("Unrecognized command %v", cmd)
	}
}

func selectorFrom(az fields.AvailabilityZone, cn fields.ClusterName, podID types.PodID) klabels.Selector {
	return klabels.Everything().
		Add(fields.PodIDLabel, klabels.EqualsOperator, []string{podID.String()}).
		Add(fields.AvailabilityZoneLabel, klabels.EqualsOperator, []string{az.String()}).
		Add(fields.ClusterNameLabel, klabels.EqualsOperator, []string{cn.String()})
}

func currentUserName() string {
	username := "unknown user"

	if user, err := user.Current(); err == nil {
		username = user.Username
	}
	return username
}
