#!/bin/bash

/go/bin/empire server --scheduler=cloudformation --automigrate=true --ecs.logdriver fluentd --ecs.logopt tag='docker.{{index .ContainerLabels "empire.app.name"}}.{{index .ContainerLabels "empire.app.process"}}'

