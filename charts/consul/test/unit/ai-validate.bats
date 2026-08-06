#!/usr/bin/env bats

load _helpers

#--------------------------------------------------------------------
# No ai block — validation must be silent

@test "ai/validate: no error when ai block is absent" {
    cd `chart_dir`
    run helm template \
        --set 'connectInject.enabled=true' \
        --set 'ai=null' \
        .
    [ "$status" -eq 0 ]
}

@test "ai/validate: no error when ai.enabled=false" {
    cd `chart_dir`
    run helm template \
        --set 'connectInject.enabled=true' \
        --set 'ai.enabled=false' \
        .
    [ "$status" -eq 0 ]
}

#--------------------------------------------------------------------
# connectInject dependency

@test "ai/validate: fails when ai.enabled=true and connectInject.enabled=false" {
    cd `chart_dir`
    run helm template \
        --set 'ai.enabled=true' \
        --set 'connectInject.enabled=false' \
        --set 'global.enabled=false' \
        .
    [ "$status" -eq 1 ]
    [[ "$output" =~ "ai.enabled=true requires connectInject.enabled=true" ]]
}

@test "ai/validate: passes when ai.enabled=true and connectInject.enabled=true" {
    cd `chart_dir`
    run helm template \
        --set 'ai.enabled=true' \
        --set 'connectInject.enabled=true' \
        .
    [ "$status" -eq 0 ]
}

#--------------------------------------------------------------------
# Reserved port 20000

@test "ai/validate: fails when inferenceModel.interceptorPort=20000" {
    cd `chart_dir`
    run helm template \
        --set 'connectInject.enabled=true' \
        --set 'ai.enabled=true' \
        --set 'ai.inferenceModel.defaults.interceptorPort=20000' \
        .
    [ "$status" -eq 1 ]
    [[ "$output" =~ "interceptorPort must not be 20000" ]]
}

@test "ai/validate: fails when mcpServer.interceptorPort=20000" {
    cd `chart_dir`
    run helm template \
        --set 'connectInject.enabled=true' \
        --set 'ai.enabled=true' \
        --set 'ai.mcpServer.defaults.interceptorPort=20000' \
        .
    [ "$status" -eq 1 ]
    [[ "$output" =~ "interceptorPort must not be 20000" ]]
}

@test "ai/validate: fails when agent.interceptorPort=20000" {
    cd `chart_dir`
    run helm template \
        --set 'connectInject.enabled=true' \
        --set 'ai.enabled=true' \
        --set 'ai.agent.defaults.interceptorPort=20000' \
        .
    [ "$status" -eq 1 ]
    [[ "$output" =~ "interceptorPort must not be 20000" ]]
}

@test "ai/validate: fails when agent.mcpPort=20000" {
    cd `chart_dir`
    run helm template \
        --set 'connectInject.enabled=true' \
        --set 'ai.enabled=true' \
        --set 'ai.agent.defaults.mcpPort=20000' \
        .
    [ "$status" -eq 1 ]
    [[ "$output" =~ "mcpPort must not be 20000" ]]
}

@test "ai/validate: fails when agent.hitl.port=20000" {
    cd `chart_dir`
    run helm template \
        --set 'connectInject.enabled=true' \
        --set 'ai.enabled=true' \
        --set 'ai.agent.defaults.hitl.port=20000' \
        .
    [ "$status" -eq 1 ]
    [[ "$output" =~ "hitl.port must not be 20000" ]]
}

#--------------------------------------------------------------------
# Port uniqueness

@test "ai/validate: fails when agent.interceptorPort == agent.mcpPort" {
    cd `chart_dir`
    run helm template \
        --set 'connectInject.enabled=true' \
        --set 'ai.enabled=true' \
        --set 'ai.agent.defaults.interceptorPort=15101' \
        --set 'ai.agent.defaults.mcpPort=15101' \
        .
    [ "$status" -eq 1 ]
    [[ "$output" =~ "interceptorPort" ]]
    [[ "$output" =~ "mcpPort" ]]
    [[ "$output" =~ "must be different ports" ]]
}

@test "ai/validate: fails when agent.interceptorPort == agent.hitl.port" {
    cd `chart_dir`
    run helm template \
        --set 'connectInject.enabled=true' \
        --set 'ai.enabled=true' \
        --set 'ai.agent.defaults.interceptorPort=16101' \
        --set 'ai.agent.defaults.hitl.port=16101' \
        .
    [ "$status" -eq 1 ]
    [[ "$output" =~ "interceptorPort" ]]
    [[ "$output" =~ "hitl.port" ]]
    [[ "$output" =~ "must be different ports" ]]
}

@test "ai/validate: fails when agent.mcpPort == agent.hitl.port" {
    cd `chart_dir`
    run helm template \
        --set 'connectInject.enabled=true' \
        --set 'ai.enabled=true' \
        --set 'ai.agent.defaults.mcpPort=16101' \
        --set 'ai.agent.defaults.hitl.port=16101' \
        .
    [ "$status" -eq 1 ]
    [[ "$output" =~ "mcpPort" ]]
    [[ "$output" =~ "hitl.port" ]]
    [[ "$output" =~ "must be different ports" ]]
}

@test "ai/validate: passes with all distinct non-reserved ports" {
    cd `chart_dir`
    run helm template \
        --set 'connectInject.enabled=true' \
        --set 'ai.enabled=true' \
        --set 'ai.agent.defaults.interceptorPort=21101' \
        --set 'ai.agent.defaults.mcpPort=15101' \
        --set 'ai.agent.defaults.hitl.port=16101' \
        .
    [ "$status" -eq 0 ]
}

#--------------------------------------------------------------------
# JSON Schema — type and enum enforcement

@test "ai/validate: JSON Schema rejects non-integer interceptorPort" {
    cd `chart_dir`
    run helm template \
        --set 'connectInject.enabled=true' \
        --set 'ai.enabled=true' \
        --set 'ai.inferenceModel.defaults.interceptorPort=not-a-number' \
        .
    [ "$status" -eq 1 ]
    [[ "$output" =~ "Error" ]]
}

@test "ai/validate: JSON Schema rejects invalid inferenceProtocol enum" {
    cd `chart_dir`
    run helm template \
        --set 'connectInject.enabled=true' \
        --set 'ai.enabled=true' \
        --set 'ai.inferenceModel.defaults.inferenceProtocol=graphql' \
        .
    [ "$status" -eq 1 ]
    [[ "$output" =~ "Error" ]]
}

@test "ai/validate: JSON Schema rejects invalid mcpServer transport enum" {
    cd `chart_dir`
    run helm template \
        --set 'connectInject.enabled=true' \
        --set 'ai.enabled=true' \
        --set 'ai.mcpServer.defaults.transport=grpc' \
        .
    [ "$status" -eq 1 ]
    [[ "$output" =~ "Error" ]]
}

@test "ai/validate: JSON Schema rejects invalid approvalTimeout pattern" {
    cd `chart_dir`
    run helm template \
        --set 'connectInject.enabled=true' \
        --set 'ai.enabled=true' \
        --set 'ai.agent.defaults.hitl.approvalTimeout=60seconds' \
        .
    [ "$status" -eq 1 ]
    [[ "$output" =~ "Error" ]]
}

@test "ai/validate: JSON Schema rejects interceptorPort below minimum (1024)" {
    cd `chart_dir`
    run helm template \
        --set 'connectInject.enabled=true' \
        --set 'ai.enabled=true' \
        --set 'ai.inferenceModel.defaults.interceptorPort=80' \
        .
    [ "$status" -eq 1 ]
    [[ "$output" =~ "Error" ]]
}

@test "ai/validate: JSON Schema rejects interceptorPort above maximum (65535)" {
    cd `chart_dir`
    run helm template \
        --set 'connectInject.enabled=true' \
        --set 'ai.enabled=true' \
        --set 'ai.inferenceModel.defaults.interceptorPort=99999' \
        .
    [ "$status" -eq 1 ]
    [[ "$output" =~ "Error" ]]
}

@test "ai/validate: JSON Schema rejects invalid memory quantity" {
    cd `chart_dir`
    run helm template \
        --set 'connectInject.enabled=true' \
        --set 'ai.enabled=true' \
        --set 'ai.agent.defaults.resources.requests.memory=lots' \
        .
    [ "$status" -eq 1 ]
    [[ "$output" =~ "Error" ]]
}
