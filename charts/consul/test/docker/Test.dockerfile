# This Dockerfile installs all the dependencies necessary to run the unit and
# acceptance tests. This image also contains gcloud so you can run tests
# against a GKE cluster easily.
#
# This image has no automatic entrypoint. It is expected that you'll run
# a script to configure kubectl, potentially install Helm, and run the tests
# manually. This image only has the dependencies pre-installed.

FROM cimg/go:1.18

# change the user to root so we can install stuff
USER root

ENV BATS_VERSION "1.6.0"
ENV TERRAFORM_VERSION "0.13.5"

RUN apt-get update

# base packages
RUN apt-get install -y \
    openssl \
    python3 \
    python3-pip \
    jq

# yq
RUN pip3 install yq

# gcloud
RUN echo "deb [signed-by=/usr/share/keyrings/cloud.google.gpg] http://packages.cloud.google.com/apt cloud-sdk main" | tee -a /etc/apt/sources.list.d/google-cloud-sdk.list && curl https://packages.cloud.google.com/apt/doc/apt-key.gpg | apt-key --keyring /usr/share/keyrings/cloud.google.gpg  add - && apt-get update -y && apt-get install google-cloud-sdk -y

# terraform
RUN curl -sSL https://releases.hashicorp.com/terraform/${TERRAFORM_VERSION}/terraform_${TERRAFORM_VERSION}_linux_amd64.zip -o /tmp/tf.zip \
    && unzip /tmp/tf.zip  \
    && mv ./terraform /usr/local/bin/terraform

# kubectl
RUN curl -LO https://storage.googleapis.com/kubernetes-release/release/$(curl -s https://storage.googleapis.com/kubernetes-release/release/stable.txt)/bin/linux/amd64/kubectl && \
    chmod +x ./kubectl && \
    mv ./kubectl /usr/local/bin/kubectl

# helm
RUN curl https://raw.githubusercontent.com/helm/helm/master/scripts/get-helm-3 | bash

# bats
RUN curl -sSL https://github.com/bats-core/bats-core/archive/v${BATS_VERSION}.tar.gz -o /tmp/bats.tgz \
    && tar -zxf /tmp/bats.tgz -C /tmp \
    && /bin/bash /tmp/bats-core-${BATS_VERSION}/install.sh /usr/local

# Azure CLI
RUN curl -sL https://aka.ms/InstallAzureCLIDeb | sudo bash

# OpenShift CLI
# https://docs.microsoft.com/en-us/azure/openshift/tutorial-connect-cluster
RUN curl -sSL https://mirror.openshift.com/pub/openshift-v4/clients/ocp/latest/openshift-client-linux.tar.gz -o /tmp/oc.tar.gz \
    && tar -zxvf /tmp/oc.tar.gz -C /tmp  \
    && mv /tmp/oc /usr/local/bin/oc

# AWS CLI
RUN curl "https://awscli.amazonaws.com/awscli-exe-linux-x86_64.zip" -o "awscliv2.zip" \
    && unzip awscliv2.zip \
    && ./aws/install --bin-dir /usr/local/bin \
    && rm awscliv2.zip \
    && rm -rf ./aws

# AWS IAM authenticator
RUN curl -o aws-iam-authenticator https://amazon-eks.s3.us-west-2.amazonaws.com/1.18.9/2020-11-02/bin/linux/amd64/aws-iam-authenticator \
    && chmod +x ./aws-iam-authenticator \
    && mv ./aws-iam-authenticator /usr/local/bin/aws-iam-authenticator

# change the user back to what circleci/golang image has
USER circleci
