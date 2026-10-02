package aap

import (
	"context"
	"net/http"
	"testing"

	"aaptui/internal/config"
)

func TestWorkflowNodes(t *testing.T) {
	c, _ := testClient(t, config.Gateway, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/controller/v2/workflow_jobs/2/workflow_nodes/" {
			t.Error(r.URL)
		}
		if _, err := w.Write([]byte(`{"count":5,"results":[{"id":1,"identifier":"pending","job":null},{"id":2,"job":null,"do_not_run":true},{"id":3,"job":10,"summary_fields":{"job":{"id":10,"type":"workflow_approval","status":"pending"}}},{"id":4,"job":11},{"id":5,"job":12,"summary_fields":{"job":{"id":12,"type":"workflow_job","status":"running"}}}]}`)); err != nil {
			t.Error(err)
		}
	})
	p, err := c.WorkflowNodes(context.Background(), JobRef{2, WorkflowJob}, ListOptions{})
	if err != nil || len(p.Items) != 5 {
		t.Fatal(p, err)
	}
	if p.Items[0].Job != nil || !p.Items[1].Skipped || p.Items[2].Job.Type != WorkflowApproval || p.Items[3].Reason == "" || p.Items[4].Job.Type != WorkflowJob {
		t.Fatal(p)
	}
}
