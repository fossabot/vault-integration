# Introduction

The following chapter describes how to setup a public key infrastructure (PKI) with Vault so, that Vault

* maintains a _root_ certificate, which may be an intermediate certificate.
* creates and signs cryptographic x.509 client and server certificates for secure TLS communication
* allows client and server to independently rotate certificates with restrictions on certificate parameters, e.g. common name and TTL

# Overview
The following steps have to be completed

* Setup a certification authority with an (optionally self-signed) root certificate
* Create Vault policies for server (cloud hub) and clients (edge nodes)
* Define required entities
* Setup kubernetes service account based authentication for the cloud hub
* Create immediate authentication tokens for the edge nodes

# PKI Configuration

The following steps have to be completed to setup a certification authority (CA) using Vault. More complete information about this process can be found [here](https://learn.hashicorp.com/tutorials/vault/pki-engine) and [here](https://learn.hashicorp.com/tutorials/vault/pki-engine-external-ca)

> Hint: Hashicorp strongly recommends, to operate a separate CA with a rarely used root certificate, that is only used to issue intermediate certificates. An intermediate certificate may be 
used to setup the CA for the "daily work".

## Authenticate to Vault

The following steps require a login to an unsealed Vault server, e.g. using 

    vault login

The call requires a Vault token, which may be the automatically generated root token (not recommended), or a dedicated admin token. The latter however requires an explicit setup of such an admin account (not covered here, see the Vault documentation)


## Basic CA setup
The following steps will create a CA using a self-signed root certificate. To use an intermediate certificate generated by a different CA, see 

        # Enable certificate secret engine
        vault secrets enable pki

        # Tune some values, see documentation for more
        vault secrets tune -max-lease-ttl $((24*365*10))h pki

        # Define some static URLs that will be integrated into the generated certificates
        vault write pki/config/urls issuing_certificates=https://vault.ci4rail.com/cert crl_distribution_points=https://vault.ci4rail.com/v1/pki/crl ocsp_servers=https://vault.ci4rail.com/ocsp

        # Create a self-signed root certificate and export it into a combined JSON file
        vault write pki/root/generate/exported common_name=ca.ci4rail.com -format=json > ca.json

        # Define a role that allows creating server certificates
        vault write pki/roles/server ext_key_usage=ServerAuth allowed_domains=ci4rail.com allow_subdomains=true
 
The generation step will create an already signed certificate, so no further handling of a CR is required.
The role defined in the final step allows to create server certificates for any subdomain of ci4rail.com. Vault allows to restrict this much further, see the documentation for details.

# Configuration for edge nodes

With the running CA the configuration for the edge nodes may be created. The following steps have to be performed:

* Define a restricted policy (ACL) that allows to request client x.509 certificates
* Define a generic role that allows clients to use the above policy
* Define a Vault entity for every edge node that defines, for which subject the node may request a certificate
* Create a login token for this entity that allows authentication of the edge nodes
* Distribute the token to the edge nodes


## Policy Creation
The first step is to create a _generic_ policy that opens access to the REST API of Vault that is required to issue new certificates:

    cat <<EOF|vault policy write pki-client -
    # Allow clients to issue PKI client certs
    path "/pki/issue/client" {
        capabilities = [ "create", "update" ]
    }
    EOF

This policy allows to _write_ to the _pki/client_ endpoint in the role "client", which will issue a suitable client certificate.


## Role Definition
With the policy in place, the referenced client role must be created:

    vault write pki/roles/client ext_key_usage=ClientAuth allowed_domains_template=true allowed_domains={{identity.entity.metadata.common_name}} allow_bare_domains=true

The above role definition is a little "tricky" and contains the following definitions:

* The resulting certificate can be used for client authentication
* The common_name passed by the client (for the definition of the certificates subject) is defined using a _template_ (using the Golang template language)
* The passed common_name must be equal to the definition of an associated entity, effectively fixing the common name that may be requested (the common_name attribute _must_ be passed when requesting a certificate)
* The final parameter _allow_bare_domains_ prevents the requesting of wildcard- or subdomain common_names

## Entity Definition
To put the role defined above to use, an entity has to be defined _for every edgenode_ that will request certificates

    vault write identity/entity name=edge0.ci4rail.com metadata=common_name=edge0.ci4rail.com

This creates a new entity named _edge0_ that has a common_name metadata attribute of 
_edge0.ci4rail.com_. With the role defined above this will limit the common_name
attribute of a certificate request to this fixed name.

## Login Token Creation
After the configuration is in place, a login token has to be created for the entity.
Following the Vault philosophy this is done in following steps:

* Create a role specific to the token authentication mechanism that defines
    * Which policies will be associated with the session identified by the resulting token
    * Define an entity alias that _bridges_ from the incoming authentication request to the selected alias (see Vault concepts)
    * Create an actual login token for the identified entity

### Create Token role
Create a role defines the resulting roles and the TTL of a token. For this example the TTL is set to 24h

    vault write auth/token/roles/pki-client allowed_policies=pki-client renewable=true token_explicit_max_ttl=24h token_no_default_policy=true allowed_entity_aliases="*.token"

### Create alias
To create the bridge from the authentication request to the entity represented by the token, an _entity alias_ must be defined:

    # Get the ID of the entity
    export ID=$(vault read -format=json identity/entity/name/edge0.ci4rail.com|jq -r .data.id)

    # The the internal identifier ('mount path') of the token authentication mechanism (may be found with 'vault auth list' as well)
    export ACCESSOR=$(vault read -format=json /sys/auth|jq -r '.data."token/".accessor')

    # Create the actual alias
    vault write identity/entity-alias name=edge0.token canonical_id=$ID mount_accessor=$ACCESSOR

### Create token

    # Finally, create a token for the entity
    vault write auth/token/create/pki-client entity_alias=edge0.token

The resulting token may have a limited TTL (24h for this test) and is distributed to the edge nodes

## Testing the Setup

After completion of all steps, it should be possible to create certificates with the token created in the last step:

    # "Login" using the token
    export VAULT_TOKEN=<the token as generated above>

    # Create a certificate bundle
    vault write -format=json pki/issue/client common_name=edge0.ci4rail.com ttl=12h | tee client.json

The resulting file "client.json" will contain

* A signed client certificate
* The certificate of the signing CA
* The private key for the client certificate
* The expiration date
* The serial number

Details of the certificate may be checked using openssl:

    cat client.json |jq -r .data.certificate|openssl x509 -text

Additionally, with the token generated above, it is not possible to invoke any other Vault functionality:

    vault policy list 
    Error listing policies: Error making API request.

    URL: GET https://vault.ci4rail.com/v1/sys/policies/acl?list=true
    Code: 403. Errors:

    * 1 error occurred:
            * permission denied

# Configuration for Cloud Hub

This describes the configuration of the PKI for usage by the cloud hub. The steps are very similar to those required
by the edge nodes
## Policy Definition

A Vault policy is required to allow access to the certification generation functionality:

    vault policy write pki-server - << EOF
    path "/pki/issue/server" {
        capabilities = [ "create","update" ]
    }
    EOF

## Role Definition

A role has to be defined, that defines the parameters

    vault write pki/roles/server ext_key_usage=ServerAuth allowed_domains=ci4rail.com allow_subdomains=true

This role defines a much more lenient settings for creating server certificates. The above example does not impose restrictions
on the certificates subject.

## Auth Method Configuration
Enable kubernetes auth method

    vault auth enable kubernetes

Configure the auth method to allow access to the cluster. For minikube:

    vault write auth/kubernetes/config kubernetes_host=https://$(minikube ip):8443 kubernetes_ca_crt=$HOME/.minikube/ca.crt

(adapt the port as needed). This must be adapted the actual cluster setup used.

The cloudcore deployment should already have created a service account:

    kubectl -n kubeedge get serviceaccount cloudcore -o yaml

if this is missing, create a new service account:
    
    kubectl -n kubeedge create serviceaccount cloudcore

Create a role that binds the serviceaccount, resp. it's secret, to a Vault role. This role defines the policies available to the serviceaccount user. Additionally, the service account(s) acccepted for kubernetes authentication are defined.
    
    vault write auth/kubernetes/role/cloudcore bound_service_account_names=cloudcore bound_service_account_namespaces=kubeedge token_policies=pki-server alias_name_source=serviceaccount_name token_no_default_policy=true


## Entity Definition
Create a new entity representing the cloudcore server

    vault write identity/entity name=cloudcore.ci4rail.com

Create an alias that associates the entity with the kubernetes login mechanism, i.e. when logging in using kubernetes the alias creates the brige to the entity


    # Determine the entity id
    export ID=$(vault read -format=json identity/entity/name/cloudcore.ci4rail.com|jq -r .data.id)

    # Determine the internal accessor id of the kubernetes auth method
    export ACCESSOR=$(vault auth list -format=json|jq -r '.["kubernetes/"].accessor')

    # Link the entity to the kubernetes auth method
    vault write identity/entity-alias canonical_id=$ID mount_accessor=$ACCESSOR name=kubeedge/cloudcore

After these steps, a pod may be using the service account secret (that has been automatically injected into the pod) to authenticate to Vault. A code example may be found [here](https://www.vaultproject.io/docs/auth/kubernetes#code-example)
