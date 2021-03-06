package main_test

import (
	"encoding/json"
	"time"

	"github.com/cloudfoundry-incubator/bbs/events"
	"github.com/cloudfoundry-incubator/bbs/models"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("Events API", func() {
	Describe("Actual LRPs", func() {
		const (
			processGuid     = "some-process-guid"
			domain          = "some-domain"
			noExpirationTTL = 0
		)

		var (
			done         chan struct{}
			eventChannel chan models.Event

			eventSource    events.EventSource
			baseLRP        *models.ActualLRP
			key            models.ActualLRPKey
			instanceKey    models.ActualLRPInstanceKey
			newInstanceKey models.ActualLRPInstanceKey
			netInfo        models.ActualLRPNetInfo
		)

		JustBeforeEach(func() {
			var err error
			eventSource, err = client.SubscribeToEvents()
			Expect(err).NotTo(HaveOccurred())

			eventChannel = make(chan models.Event)
			done = make(chan struct{})

			go func() {
				defer close(done)
				for {
					event, err := eventSource.Next()
					if err != nil {
						close(eventChannel)
						return
					}
					eventChannel <- event
				}
			}()

			rawMessage := json.RawMessage([]byte(`{"port":8080,"hosts":["primer-route"]}`))
			primerLRP := &models.DesiredLRP{
				ProcessGuid: "primer-guid",
				Domain:      "primer-domain",
				RootFs:      "primer:rootfs",
				Routes: &models.Routes{
					"router": &rawMessage,
				},
				Action: models.WrapAction(&models.RunAction{
					User: "me",
					Path: "true",
				}),
			}

		PRIMING:
			for {
				select {
				case <-eventChannel:
					break PRIMING
				case <-time.After(50 * time.Millisecond):
					etcdHelper.SetRawDesiredLRP(primerLRP)
					Expect(err).NotTo(HaveOccurred())
				}
			}
		})

		BeforeEach(func() {
			key = models.NewActualLRPKey(processGuid, 0, domain)
			instanceKey = models.NewActualLRPInstanceKey("instance-guid", "cell-id")
			newInstanceKey = models.NewActualLRPInstanceKey("other-instance-guid", "other-cell-id")
			netInfo = models.NewActualLRPNetInfo("1.1.1.1")

			baseLRP = &models.ActualLRP{
				ActualLRPKey:         key,
				ActualLRPInstanceKey: instanceKey,
				ActualLRPNetInfo:     netInfo,
				State:                models.ActualLRPStateRunning,
				Since:                time.Now().UnixNano(),
			}
		})

		It("receives events", func() {
			By("creating a ActualLRP")
			etcdHelper.SetRawActualLRP(baseLRP)

			actualLRPGroup, err := client.ActualLRPGroupByProcessGuidAndIndex(processGuid, 0)
			Expect(err).NotTo(HaveOccurred())

			var event models.Event
			Eventually(func() models.Event {
				Eventually(eventChannel).Should(Receive(&event))
				return event
			}).Should(BeAssignableToTypeOf(&models.ActualLRPCreatedEvent{}))

			actualLRPCreatedEvent := event.(*models.ActualLRPCreatedEvent)
			Expect(actualLRPCreatedEvent.ActualLrpGroup).To(Equal(actualLRPGroup))

			By("updating the existing ActualLRP")
			_, err = client.ClaimActualLRP(processGuid, int(key.Index), &instanceKey)
			Expect(err).NotTo(HaveOccurred())

			before := actualLRPGroup
			actualLRPGroup, err = client.ActualLRPGroupByProcessGuidAndIndex(processGuid, 0)
			Expect(err).NotTo(HaveOccurred())

			Eventually(func() models.Event {
				Eventually(eventChannel).Should(Receive(&event))
				return event
			}).Should(BeAssignableToTypeOf(&models.ActualLRPChangedEvent{}))

			actualLRPChangedEvent := event.(*models.ActualLRPChangedEvent)
			Expect(actualLRPChangedEvent.Before).To(Equal(before))
			Expect(actualLRPChangedEvent.After).To(Equal(actualLRPGroup))

			Skip("not possible to fully implement this yet")

			// By("evacuating the ActualLRP")
			// _, err = legacyBBS.EvacuateRunningActualLRP(logger, key, instanceKey, netInfo, 0)
			// Expect(err).To(Equal(bbserrors.ErrServiceUnavailable))

			// etcdHelper.SetRawEvacuatingActualLRP(baseLRP, noExpirationTTL)
			// evacuatingLRPGroup, err := client.ActualLRPGroupByProcessGuidAndIndex(processGuid, 0)
			// Expect(err).NotTo(HaveOccurred())
			// evacuatingLRP := *evacuatingLRPGroup.GetEvacuating()

			// Eventually(func() receptor.Event {
			// 	Eventually(events).Should(Receive(&event))
			// 	return event
			// }).Should(BeAssignableToTypeOf(receptor.ActualLRPCreatedEvent{}))

			// // this is a necessary hack until we migrate other things to protobufs or pointer structs
			// actualLRPCreatedEvent = event.(receptor.ActualLRPCreatedEvent)
			// response := actualLRPCreatedEvent.ActualLRPResponse
			// response.Ports = nil
			// Expect(response).To(Equal(serialization.ActualLRPProtoToResponse(evacuatingLRP, true)))

			// // discard instance -> UNCLAIMED
			// Eventually(func() receptor.Event {
			// 	Eventually(events).Should(Receive(&event))
			// 	return event
			// }).Should(BeAssignableToTypeOf(receptor.ActualLRPChangedEvent{}))

			// By("starting and then evacuating the ActualLRP on another cell")
			// err = legacyBBS.StartActualLRP(logger, key, newInstanceKey, netInfo)
			// Expect(err).NotTo(HaveOccurred())

			// // discard instance -> RUNNING
			// Eventually(func() receptor.Event {
			// 	Eventually(events).Should(Receive(&event))
			// 	return event
			// }).Should(BeAssignableToTypeOf(receptor.ActualLRPChangedEvent{}))

			// evacuatingBefore := evacuatingLRP
			// _, err = legacyBBS.EvacuateRunningActualLRP(logger, key, newInstanceKey, netInfo, 0)
			// Expect(err).To(Equal(bbserrors.ErrServiceUnavailable))

			// evacuatingLRPGroup, err = bbsClient.ActualLRPGroupByProcessGuidAndIndex(desiredLRP.ProcessGuid, 0)
			// Expect(err).NotTo(HaveOccurred())
			// evacuatingLRP = *evacuatingLRPGroup.GetEvacuating()

			// Expect(err).NotTo(HaveOccurred())

			// Eventually(func() receptor.Event {
			// 	Eventually(events).Should(Receive(&event))
			// 	return event
			// }).Should(BeAssignableToTypeOf(receptor.ActualLRPChangedEvent{}))

			// actualLRPChangedEvent = event.(receptor.ActualLRPChangedEvent)
			// response = actualLRPChangedEvent.Before
			// response.Ports = nil
			// Expect(response).To(Equal(serialization.ActualLRPProtoToResponse(evacuatingBefore, true)))

			// response = actualLRPChangedEvent.After
			// response.Ports = nil
			// Expect(response).To(Equal(serialization.ActualLRPProtoToResponse(evacuatingLRP, true)))

			// // discard instance -> UNCLAIMED
			// Eventually(func() receptor.Event {
			// 	Eventually(events).Should(Receive(&event))
			// 	return event
			// }).Should(BeAssignableToTypeOf(receptor.ActualLRPChangedEvent{}))

			// By("removing the instance ActualLRP")
			// actualLRPGroup, err = bbsClient.ActualLRPGroupByProcessGuidAndIndex(desiredLRP.ProcessGuid, 0)
			// Expect(err).NotTo(HaveOccurred())
			// actualLRP = *actualLRPGroup.Instance

			// err = legacyBBS.RemoveActualLRP(logger, key, models.ActualLRPInstanceKey{})
			// Expect(err).NotTo(HaveOccurred())

			// Eventually(func() receptor.Event {
			// 	Eventually(events).Should(Receive(&event))
			// 	return event
			// }).Should(BeAssignableToTypeOf(receptor.ActualLRPRemovedEvent{}))

			// // this is a necessary hack until we migrate other things to protobufs or pointer structs
			// actualLRPRemovedEvent := event.(receptor.ActualLRPRemovedEvent)
			// response = actualLRPRemovedEvent.ActualLRPResponse
			// response.Ports = nil
			// Expect(response).To(Equal(serialization.ActualLRPProtoToResponse(actualLRP, false)))

			// By("removing the evacuating ActualLRP")
			// err = legacyBBS.RemoveEvacuatingActualLRP(logger, key, newInstanceKey)
			// Expect(err).NotTo(HaveOccurred())

			// Eventually(func() receptor.Event {
			// 	Eventually(events).Should(Receive(&event))
			// 	return event
			// }).Should(BeAssignableToTypeOf(receptor.ActualLRPRemovedEvent{}))

			// Expect(event).To(BeAssignableToTypeOf(receptor.ActualLRPRemovedEvent{}))

			// // this is a necessary hack until we migrate other things to protobufs or pointer structs
			// actualLRPRemovedEvent = event.(receptor.ActualLRPRemovedEvent)
			// response = actualLRPRemovedEvent.ActualLRPResponse
			// response.Ports = nil
			// Expect(response).To(Equal(serialization.ActualLRPProtoToResponse(evacuatingLRP, true)))
		})
	})
})
