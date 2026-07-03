apiVersion: v1
kind: Secret
metadata:
  name: multica-local-runtime-auth
  namespace: ${NAMESPACE}
type: Opaque
stringData:
  runtime_token: ${MULTICA_RUNTIME_TOKEN}
  workspace_id: ${MULTICA_WORKSPACE_ID}
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: multica-runtime-bootstrap
  namespace: ${NAMESPACE}
data:
  runtime-bootstrap.sh: |
    ${BOOTSTRAP_SCRIPT}
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: multica-codex-go-runtime
  namespace: ${NAMESPACE}
  labels:
    app: multica-codex-go-runtime
spec:
  replicas: 1
  selector:
    matchLabels:
      app: multica-codex-go-runtime
  template:
    metadata:
      labels:
        app: multica-codex-go-runtime
    spec:
      hostNetwork: true
      dnsPolicy: ClusterFirstWithHostNet
      hostname: multica-codex-go-runtime
      nodeSelector:
        kubernetes.io/hostname: ${GO_NODE}
      tolerations:
        - key: zeus
          operator: Equal
          value: pre
          effect: NoSchedule
      containers:
        - name: runtime
          image: ${GO_IMAGE}
          imagePullPolicy: Always
          command: ["/usr/bin/bash", "/opt/runtime-bootstrap/runtime-bootstrap.sh"]
          env:
            - name: MULTICA_SERVER_URL
              value: ${MULTICA_SERVER_URL}
            - name: MULTICA_BINARY_URL
              value: ${MULTICA_BINARY_URL}
            - name: RUNTIME_FLAVOR
              value: go
            - name: MULTICA_RUNTIME_TOKEN
              valueFrom:
                secretKeyRef:
                  name: multica-local-runtime-auth
                  key: runtime_token
            - name: MULTICA_WORKSPACE_ID
              valueFrom:
                secretKeyRef:
                  name: multica-local-runtime-auth
                  key: workspace_id
            - name: MULTICA_DAEMON_ID
              value: multica-codex-go-runtime
            - name: MULTICA_DAEMON_DEVICE_NAME
              value: k8s-codex-go
            - name: MULTICA_AGENT_RUNTIME_NAME
              value: Codex Go Runtime
            - name: MULTICA_DAEMON_MAX_CONCURRENT_TASKS
              value: "2"
            - name: MULTICA_DAEMON_POLL_INTERVAL
              value: 3s
            - name: MULTICA_DAEMON_HEARTBEAT_INTERVAL
              value: 15s
            - name: MULTICA_WORKSPACES_ROOT
              value: /workspaces
          envFrom:
            - secretRef:
                name: claude-code-company-env
          resources:
            requests:
              cpu: 500m
              memory: 1Gi
            limits:
              cpu: "2"
              memory: 4Gi
          volumeMounts:
            - name: codex-config
              mountPath: /opt/codex-config
              readOnly: true
            - name: runtime-bootstrap
              mountPath: /opt/runtime-bootstrap
              readOnly: true
            - name: workspaces
              mountPath: /workspaces
      volumes:
        - name: codex-config
          configMap:
            name: codex-config
        - name: runtime-bootstrap
          configMap:
            name: multica-runtime-bootstrap
            defaultMode: 0755
        - name: workspaces
          emptyDir: {}
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: multica-codex-python-runtime
  namespace: ${NAMESPACE}
  labels:
    app: multica-codex-python-runtime
spec:
  replicas: 1
  selector:
    matchLabels:
      app: multica-codex-python-runtime
  template:
    metadata:
      labels:
        app: multica-codex-python-runtime
    spec:
      hostNetwork: true
      dnsPolicy: ClusterFirstWithHostNet
      hostname: multica-codex-python-runtime
      nodeSelector:
        kubernetes.io/hostname: ${PYTHON_NODE}
      tolerations:
        - key: zeus
          operator: Equal
          value: pre
          effect: NoSchedule
      containers:
        - name: runtime
          image: ${PYTHON_IMAGE}
          imagePullPolicy: Always
          command: ["/usr/bin/bash", "/opt/runtime-bootstrap/runtime-bootstrap.sh"]
          env:
            - name: MULTICA_SERVER_URL
              value: ${MULTICA_SERVER_URL}
            - name: MULTICA_BINARY_URL
              value: ${MULTICA_BINARY_URL}
            - name: RUNTIME_FLAVOR
              value: python
            - name: MULTICA_RUNTIME_TOKEN
              valueFrom:
                secretKeyRef:
                  name: multica-local-runtime-auth
                  key: runtime_token
            - name: MULTICA_WORKSPACE_ID
              valueFrom:
                secretKeyRef:
                  name: multica-local-runtime-auth
                  key: workspace_id
            - name: MULTICA_DAEMON_ID
              value: multica-codex-python-runtime
            - name: MULTICA_DAEMON_DEVICE_NAME
              value: k8s-codex-python
            - name: MULTICA_AGENT_RUNTIME_NAME
              value: Codex Python Runtime
            - name: MULTICA_DAEMON_MAX_CONCURRENT_TASKS
              value: "2"
            - name: MULTICA_DAEMON_POLL_INTERVAL
              value: 3s
            - name: MULTICA_DAEMON_HEARTBEAT_INTERVAL
              value: 15s
            - name: MULTICA_WORKSPACES_ROOT
              value: /workspaces
          envFrom:
            - secretRef:
                name: claude-code-company-env
          resources:
            requests:
              cpu: 500m
              memory: 1Gi
            limits:
              cpu: "2"
              memory: 4Gi
          volumeMounts:
            - name: codex-config
              mountPath: /opt/codex-config
              readOnly: true
            - name: runtime-bootstrap
              mountPath: /opt/runtime-bootstrap
              readOnly: true
            - name: workspaces
              mountPath: /workspaces
      volumes:
        - name: codex-config
          configMap:
            name: codex-config
        - name: runtime-bootstrap
          configMap:
            name: multica-runtime-bootstrap
            defaultMode: 0755
        - name: workspaces
          emptyDir: {}
