// 临时工具：推送测试呼叫任务到 Asynq 队列。
package main

import (
	"encoding/json"
	"fmt"
	"log"

	"github.com/hibiken/asynq"

	"github.com/omeyang/clarion/internal/scheduler"
)

func main() {
	client := asynq.NewClient(asynq.RedisClientOpt{
		Addr: "127.0.0.1:6379",
	})
	defer client.Close()

	payload := scheduler.OutboundCallPayload{
		TenantID:   "019cece3-76da-719b-bffa-4048f6c322bb",
		CallID:     1,
		ContactID:  1,
		TaskID:     1,
		Phone:      "1000",
		Gateway:    "local",
		CallerID:   "8888",
		TemplateID: 2,
		AttemptNo:  1,
	}

	task, err := scheduler.NewOutboundCallTask(payload)
	if err != nil {
		log.Fatal(err)
	}

	info, err := client.Enqueue(task, asynq.Queue("outbound"))
	if err != nil {
		log.Fatal(err)
	}

	data, _ := json.MarshalIndent(payload, "", "  ")
	fmt.Printf("任务已推送：id=%s queue=%s\n%s\n", info.ID, info.Queue, data)
}
