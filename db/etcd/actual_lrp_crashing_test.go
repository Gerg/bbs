package etcd_test

import (
	"fmt"
	"time"

	"github.com/cloudfoundry-incubator/bbs/db/etcd"
	"github.com/cloudfoundry-incubator/bbs/models"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

const OverTime = models.CrashResetTimeout + time.Minute

var _ = Describe("CrashActualLRP", func() {
	var crashTests = []crashTest{
		{
			Name: "when the lrp is RUNNING and the crash count is greater than 3",
			LRP: func() models.ActualLRP {
				lrp := lrpForState(models.ActualLRPStateRunning, time.Minute)
				lrp.CrashCount = 4
				return lrp
			},
			Result: itCrashesTheLRP(),
		},
		{
			Name: "when the lrp is RUNNING and the crash count is less than 3",
			LRP: func() models.ActualLRP {
				return lrpForState(models.ActualLRPStateRunning, time.Minute)
			},
			Result: itUnclaimsTheLRP(),
		},
		{
			Name: "when the lrp is RUNNING and has crashes and Since is older than 5 minutes",
			LRP: func() models.ActualLRP {
				lrp := lrpForState(models.ActualLRPStateRunning, OverTime)
				lrp.CrashCount = 4
				return lrp
			},
			Result: itUnclaimsTheLRP(),
		},
	}

	crashTests = append(crashTests, resetOnlyRunningLRPsThatHaveNotCrashedRecently()...)

	for _, t := range crashTests {
		var crashTest = t
		crashTest.Test()
	}
})

func resetOnlyRunningLRPsThatHaveNotCrashedRecently() []crashTest {
	lrpGenerator := func(state string) lrpSetupFunc {
		return func() models.ActualLRP {
			lrp := lrpForState(state, OverTime)
			lrp.CrashCount = 4
			return lrp
		}
	}

	nameGenerator := func(state string) string {
		return fmt.Sprintf("when the lrp is %s and has crashes and Since is older than 5 minutes", state)
	}

	tests := []crashTest{
		crashTest{
			Name:   nameGenerator(models.ActualLRPStateUnclaimed),
			LRP:    lrpGenerator(models.ActualLRPStateUnclaimed),
			Result: itDoesNotChangeTheUnclaimedLRP(),
		},
		crashTest{
			Name:   nameGenerator(models.ActualLRPStateClaimed),
			LRP:    lrpGenerator(models.ActualLRPStateClaimed),
			Result: itCrashesTheLRP(),
		},
		crashTest{
			Name:   nameGenerator(models.ActualLRPStateRunning),
			LRP:    lrpGenerator(models.ActualLRPStateRunning),
			Result: itUnclaimsTheLRP(),
		},
		crashTest{
			Name:   nameGenerator(models.ActualLRPStateCrashed),
			LRP:    lrpGenerator(models.ActualLRPStateCrashed),
			Result: itDoesNotChangeTheCrashedLRP(),
		},
	}

	return tests
}

type lrpSetupFunc func() models.ActualLRP

type crashTest struct {
	Name   string
	LRP    lrpSetupFunc
	Result crashTestResult
}

type crashTestResult struct {
	State        string
	CrashCount   int32
	CrashReason  string
	ShouldUpdate bool
	Auction      bool
	ReturnedErr  error
}

func itUnclaimsTheLRP() crashTestResult {
	return crashTestResult{
		CrashCount:   1,
		CrashReason:  "crashed",
		State:        models.ActualLRPStateUnclaimed,
		ShouldUpdate: true,
		Auction:      true,
		ReturnedErr:  nil,
	}
}

func itCrashesTheLRP() crashTestResult {
	return crashTestResult{
		CrashCount:   5,
		CrashReason:  "crashed",
		State:        models.ActualLRPStateCrashed,
		ShouldUpdate: true,
		Auction:      false,
		ReturnedErr:  nil,
	}
}

func itDoesNotChangeTheUnclaimedLRP() crashTestResult {
	return crashTestResult{
		CrashCount:   4,
		State:        models.ActualLRPStateUnclaimed,
		ShouldUpdate: false,
		Auction:      false,
		ReturnedErr:  models.ErrActualLRPCannotBeCrashed,
	}
}

func itDoesNotChangeTheCrashedLRP() crashTestResult {
	return crashTestResult{
		CrashCount:   4,
		CrashReason:  "crashed",
		State:        models.ActualLRPStateCrashed,
		ShouldUpdate: false,
		Auction:      false,
		ReturnedErr:  models.ErrActualLRPCannotBeCrashed,
	}
}

func (t crashTest) Test() {
	Context(t.Name, func() {
		var (
			crashErr                 error
			crashRequest             *models.CrashActualLRPRequest
			actualLRPKey             *models.ActualLRPKey
			instanceKey              *models.ActualLRPInstanceKey
			initialTimestamp         int64
			initialModificationIndex uint32
		)

		BeforeEach(func() {
			actualLRP := t.LRP()
			actualLRPKey = &actualLRP.ActualLRPKey
			instanceKey = &actualLRP.ActualLRPInstanceKey

			initialTimestamp = actualLRP.Since
			initialModificationIndex = actualLRP.ModificationTag.Index

			desiredLRP := models.DesiredLRP{
				ProcessGuid: actualLRPKey.ProcessGuid,
				Domain:      actualLRPKey.Domain,
				Instances:   actualLRPKey.Index + 1,
				RootFs:      "foo:bar",
				Action:      models.WrapAction(&models.RunAction{Path: "true", User: "me"}),
			}

			crashRequest = &models.CrashActualLRPRequest{
				ActualLrpKey:         &actualLRP.ActualLRPKey,
				ActualLrpInstanceKey: &actualLRP.ActualLRPInstanceKey,
				ErrorMessage:         "crashed",
			}

			etcdHelper.SetRawDesiredLRP(&desiredLRP)
			etcdHelper.SetRawActualLRP(&actualLRP)
		})

		JustBeforeEach(func() {
			clock.Increment(600)
			crashErr = etcdDB.CrashActualLRP(logger, crashRequest)
		})

		if t.Result.ReturnedErr == nil {
			It("does not return an error", func() {
				Expect(crashErr).NotTo(HaveOccurred())
			})
		} else {
			It(fmt.Sprintf("returned error should be '%s'", t.Result.ReturnedErr.Error()), func() {
				Expect(crashErr).To(Equal(t.Result.ReturnedErr))
			})
		}

		It(fmt.Sprintf("has crash count %d", t.Result.CrashCount), func() {
			actualLRP, err := etcdHelper.GetInstanceActualLRP(actualLRPKey)
			Expect(err).NotTo(HaveOccurred())
			Expect(actualLRP.CrashCount).To(Equal(t.Result.CrashCount))
		})

		It(fmt.Sprintf("has crash reason %s", t.Result.CrashReason), func() {
			actualLRP, err := etcdHelper.GetInstanceActualLRP(actualLRPKey)
			Expect(err).NotTo(HaveOccurred())
			Expect(actualLRP.CrashReason).To(Equal(t.Result.CrashReason))
		})

		if t.Result.ShouldUpdate {
			It("updates the Since", func() {
				actualLRP, err := etcdHelper.GetInstanceActualLRP(actualLRPKey)
				Expect(err).NotTo(HaveOccurred())
				Expect(actualLRP.Since).To(Equal(clock.Now().UnixNano()))
			})

			It("updates the ModificationIndex", func() {
				actualLRP, err := etcdHelper.GetInstanceActualLRP(actualLRPKey)
				Expect(err).NotTo(HaveOccurred())
				Expect(actualLRP.ModificationTag.Index).To(Equal(initialModificationIndex + 1))
			})
		} else {
			It("does not update the Since", func() {
				actualLRP, err := etcdHelper.GetInstanceActualLRP(actualLRPKey)
				Expect(err).NotTo(HaveOccurred())
				Expect(actualLRP.Since).To(Equal(initialTimestamp))
			})

			It("does not update the ModificationIndex", func() {
				actualLRP, err := etcdHelper.GetInstanceActualLRP(actualLRPKey)
				Expect(err).NotTo(HaveOccurred())
				Expect(actualLRP.ModificationTag.Index).To(Equal(initialModificationIndex))
			})
		}

		It(fmt.Sprintf("CAS to %s", t.Result.State), func() {
			actualLRP, err := etcdHelper.GetInstanceActualLRP(actualLRPKey)
			Expect(err).NotTo(HaveOccurred())
			Expect(actualLRP.State).To(Equal(t.Result.State))
		})

		if t.Result.Auction {
			It("starts an auction", func() {
				Expect(auctioneerClient.RequestLRPAuctionsCallCount()).To(Equal(1))

				requestedAuctions := auctioneerClient.RequestLRPAuctionsArgsForCall(0)
				Expect(requestedAuctions).To(HaveLen(1))

				desiredLRP, err := etcdDB.DesiredLRPByProcessGuid(logger, actualLRPKey.ProcessGuid)
				Expect(err).NotTo(HaveOccurred())
				Expect(requestedAuctions[0].DesiredLRP).To(Equal(desiredLRP))
				Expect(requestedAuctions[0].Indices).To(ConsistOf(uint(actualLRPKey.Index)))
			})

			Context("when the desired LRP no longer exists", func() {
				BeforeEach(func() {
					_, err := etcdClient.Delete(etcd.DesiredLRPSchemaPathByProcessGuid(actualLRPKey.ProcessGuid), false)
					Expect(err).NotTo(HaveOccurred())
				})

				It("the actual LRP is also deleted", func() {
					Expect(crashErr).NotTo(HaveOccurred())

					_, err := etcdDB.ActualLRPGroupByProcessGuidAndIndex(logger, actualLRPKey.ProcessGuid, actualLRPKey.Index)
					Expect(err).To(Equal(models.ErrResourceNotFound))
				})
			})
		} else {
			It("does not start an auction", func() {
				Expect(auctioneerClient.RequestLRPAuctionsCallCount()).To(Equal(0))
			})
		}

		Context("when crashing a different instance key", func() {
			var beforeActualGroup *models.ActualLRPGroup

			BeforeEach(func() {
				var err error
				beforeActualGroup, err = etcdDB.ActualLRPGroupByProcessGuidAndIndex(logger, actualLRPKey.ProcessGuid, actualLRPKey.Index)
				Expect(err).NotTo(HaveOccurred())
				instanceKey.InstanceGuid = "another-guid"
			})

			It("does not crash", func() {
				Expect(crashErr).To(Equal(models.ErrActualLRPCannotBeCrashed))

				afterActualGroup, err := etcdDB.ActualLRPGroupByProcessGuidAndIndex(logger, actualLRPKey.ProcessGuid, actualLRPKey.Index)
				Expect(err).NotTo(HaveOccurred())
				Expect(afterActualGroup).To(Equal(beforeActualGroup))
			})
		})
	})
}

func lrpForState(state string, timeInState time.Duration) models.ActualLRP {
	var actualLRPKey = models.NewActualLRPKey("some-process-guid", 1, "tests")
	var instanceKey = models.NewActualLRPInstanceKey("some-instance-guid", "some-cell")

	lrp := models.ActualLRP{
		ActualLRPKey: actualLRPKey,
		State:        state,
		Since:        clock.Now().Add(-timeInState).UnixNano(),
	}

	switch state {
	case models.ActualLRPStateUnclaimed:
	case models.ActualLRPStateCrashed:
		lrp.CrashReason = "crashed"
	case models.ActualLRPStateClaimed:
		lrp.ActualLRPInstanceKey = instanceKey
	case models.ActualLRPStateRunning:
		lrp.ActualLRPInstanceKey = instanceKey
		lrp.ActualLRPNetInfo = models.NewActualLRPNetInfo("1.2.3.4", &models.PortMapping{ContainerPort: 1234, HostPort: 5678})
	}

	return lrp
}
