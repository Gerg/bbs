package main_test

import (
	"github.com/cloudfoundry-incubator/bbs/models"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("Task API", func() {
	Context("Getters", func() {
		var expectedTasks []*models.Task

		BeforeEach(func() {
			expectedTasks = []*models.Task{testHelper.NewValidTask("a-guid"), testHelper.NewValidTask("b-guid")}
			expectedTasks[1].Domain = "b-domain"
			expectedTasks[1].CellId = "b-cell"
			for _, t := range expectedTasks {
				testHelper.SetRawTask(t)
			}
		})

		Describe("GET /v1/tasks", func() {
			Context("all tasks", func() {
				It("has the correct number of responses", func() {
					actualTasks, err := client.Tasks()
					Expect(err).NotTo(HaveOccurred())
					Expect(actualTasks).To(ConsistOf(expectedTasks))
				})
			})

			Context("when filtering by domain", func() {
				It("has the correct number of responses", func() {
					domain := expectedTasks[0].Domain
					actualTasks, err := client.TasksByDomain(domain)
					Expect(err).NotTo(HaveOccurred())
					Expect(actualTasks).To(ConsistOf(expectedTasks[0]))
				})
			})

			Context("when filtering by cell", func() {
				It("has the correct number of responses", func() {
					actualTasks, err := client.TasksByCellID("b-cell")
					Expect(err).NotTo(HaveOccurred())
					Expect(actualTasks).To(ConsistOf(expectedTasks[1]))
				})
			})
		})

		Describe("GET /v1/tasks/:task_guid", func() {
			It("returns the task", func() {
				task, err := client.TaskByGuid(expectedTasks[0].TaskGuid)
				Expect(err).NotTo(HaveOccurred())
				Expect(task).To(Equal(expectedTasks[0]))
			})
		})
	})

	Context("Setters", func() {
		Describe("POST /v1/tasks/", func() {
			It("adds the desired task", func() {
				expectedTask := testHelper.NewValidTask("task-1")
				err := client.DesireTask(expectedTask.TaskGuid, expectedTask.Domain, expectedTask.TaskDefinition)
				Expect(err).NotTo(HaveOccurred())

				task, err := client.TaskByGuid(expectedTask.TaskGuid)
				Expect(err).NotTo(HaveOccurred())
				Expect(task.TaskDefinition).To(Equal(expectedTask.TaskDefinition))
			})
		})
	})
})
