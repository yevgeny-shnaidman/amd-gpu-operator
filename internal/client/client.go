package client

import _ "go.uber.org/mock/mockgen/model"

//go:generate mockgen -package=client -destination mock_client.go sigs.k8s.io/controller-runtime/pkg/client Client,StatusWriter
