// Copyright IBM Corp. 2023 All Rights Reserved.
// Licensed under the Mozilla Public License v2.0

package secretsmanager

import (
	"context"
	"fmt"
	"github.com/IBM-Cloud/bluemix-go/bmxerror"
	"github.com/hashicorp/terraform-plugin-sdk/v2/diag"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"log"
	"strings"
	"time"

	"github.com/IBM-Cloud/terraform-provider-ibm/ibm/conns"
	"github.com/IBM-Cloud/terraform-provider-ibm/ibm/flex"
	"github.com/IBM/go-sdk-core/v5/core"
	"github.com/IBM/secrets-manager-go-sdk/secretsmanagerv2"
)

func ResourceIbmSmKvSecret() *schema.Resource {
	return &schema.Resource{
		CreateContext: resourceIbmSmKvSecretCreate,
		ReadContext:   resourceIbmSmKvSecretRead,
		UpdateContext: resourceIbmSmKvSecretUpdate,
		DeleteContext: resourceIbmSmKvSecretDelete,
		Importer:      &schema.ResourceImporter{},

		Schema: map[string]*schema.Schema{
			"secret_type": &schema.Schema{
				Type:        schema.TypeString,
				Computed:    true,
				Description: "The secret type. Supported types are arbitrary, certificates (imported, public, and private), IAM credentials, key-value, and user credentials.",
			},
			"name": &schema.Schema{
				Type:        schema.TypeString,
				Required:    true,
				Description: "A human-readable name to assign to your secret.To protect your privacy, do not use personal data, such as your name or location, as a name for your secret.",
			},
			"description": &schema.Schema{
				Type:        schema.TypeString,
				Optional:    true,
				Description: "An extended description of your secret.To protect your privacy, do not use personal data, such as your name or location, as a description for your secret group.",
			},
			"secret_group_id": &schema.Schema{
				Type:        schema.TypeString,
				Optional:    true,
				Computed:    true,
				ForceNew:    true,
				Description: "A v4 UUID identifier, or `default` secret group.",
			},
			"labels": &schema.Schema{
				Type:        schema.TypeList,
				Optional:    true,
				Description: "Labels that you can use to search for secrets in your instance.Up to 30 labels can be created.",
				Elem:        &schema.Schema{Type: schema.TypeString},
			},
			"data": &schema.Schema{
				Type:        schema.TypeMap,
				Required:    true,
				ForceNew:    true,
				Sensitive:   true,
				Description: "The payload data of a key-value secret.",
				Elem:        &schema.Schema{Type: schema.TypeString},
			},
			"custom_metadata": &schema.Schema{
				Type:        schema.TypeMap,
				Optional:    true,
				Description: "The secret metadata that a user can customize.",
				Elem:        &schema.Schema{Type: schema.TypeString},
			},
			"version_custom_metadata": &schema.Schema{
				Type:        schema.TypeMap,
				Optional:    true,
				ForceNew:    true,
				Description: "The secret version metadata that a user can customize.",
				Elem:        &schema.Schema{Type: schema.TypeString},
			},
			"created_by": &schema.Schema{
				Type:        schema.TypeString,
				Computed:    true,
				Description: "The unique identifier that is associated with the entity that created the secret.",
			},
			"created_at": &schema.Schema{
				Type:        schema.TypeString,
				Computed:    true,
				Description: "The date when a resource was created. The date format follows RFC 3339.",
			},
			"crn": &schema.Schema{
				Type:        schema.TypeString,
				Computed:    true,
				Description: "A CRN that uniquely identifies an IBM Cloud resource.",
			},
			"downloaded": &schema.Schema{
				Type:        schema.TypeBool,
				Computed:    true,
				Description: "Indicates whether the secret data that is associated with a secret version was retrieved in a call to the service API.",
			},
			"locks_total": &schema.Schema{
				Type:        schema.TypeInt,
				Computed:    true,
				Description: "The number of locks of the secret.",
			},
			"state": &schema.Schema{
				Type:        schema.TypeInt,
				Computed:    true,
				Description: "The secret state that is based on NIST SP 800-57. States are integers and correspond to the `Pre-activation = 0`, `Active = 1`,  `Suspended = 2`, `Deactivated = 3`, and `Destroyed = 5` values.",
			},
			"state_description": &schema.Schema{
				Type:        schema.TypeString,
				Computed:    true,
				Description: "A text representation of the secret state.",
			},
			"updated_at": &schema.Schema{
				Type:        schema.TypeString,
				Computed:    true,
				Description: "The date when a resource was recently modified. The date format follows RFC 3339.",
			},
			"versions_total": &schema.Schema{
				Type:        schema.TypeInt,
				Computed:    true,
				Description: "The number of versions of the secret.",
			},
			"secret_id": &schema.Schema{
				Type:        schema.TypeString,
				Computed:    true,
				Description: "A v4 UUID identifier.",
			},
		},
	}
}

func resourceIbmSmKvSecretCreate(context context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	secretsManagerClient, err := meta.(conns.ClientSession).SecretsManagerV2()
	if err != nil {
		return diag.FromErr(err)
	}

	region := getRegion(secretsManagerClient, d)
	instanceId := d.Get("instance_id").(string)
	secretsManagerClient = getClientWithInstanceEndpoint(secretsManagerClient, instanceId, region, getEndpointType(secretsManagerClient, d))

	createSecretOptions := &secretsmanagerv2.CreateSecretOptions{}

	secretPrototypeModel, err := resourceIbmSmKvSecretMapToSecretPrototype(d)
	if err != nil {
		return diag.FromErr(err)
	}
	createSecretOptions.SetSecretPrototype(secretPrototypeModel)

	secretIntf, response, err := secretsManagerClient.CreateSecretWithContext(context, createSecretOptions)
	if err != nil {
		log.Printf("[DEBUG] CreateSecretWithContext failed %s\n%s", err, response)
		return diag.FromErr(fmt.Errorf("CreateSecretWithContext failed %s\n%s", err, response))
	}

	secret := secretIntf.(*secretsmanagerv2.KVSecret)
	d.SetId(fmt.Sprintf("%s/%s/%s", region, instanceId, *secret.ID))
	d.Set("secret_id", *secret.ID)

	_, err = waitForIbmSmKvSecretCreate(secretsManagerClient, d)
	if err != nil {
		return diag.FromErr(fmt.Errorf(
			"Error waiting for resource IbmSmKvSecret (%s) to be created: %s", d.Id(), err))
	}

	return resourceIbmSmKvSecretRead(context, d, meta)
}

func waitForIbmSmKvSecretCreate(secretsManagerClient *secretsmanagerv2.SecretsManagerV2, d *schema.ResourceData) (interface{}, error) {
	getSecretOptions := &secretsmanagerv2.GetSecretOptions{}

	id := strings.Split(d.Id(), "/")
	secretId := id[2]

	getSecretOptions.SetID(secretId)

	stateConf := &resource.StateChangeConf{
		Pending: []string{"pre_activation"},
		Target:  []string{"active"},
		Refresh: func() (interface{}, string, error) {
			stateObjIntf, response, err := secretsManagerClient.GetSecret(getSecretOptions)
			stateObj := stateObjIntf.(*secretsmanagerv2.KVSecret)
			if err != nil {
				if apiErr, ok := err.(bmxerror.RequestFailure); ok && apiErr.StatusCode() == 404 {
					return nil, "", fmt.Errorf("The instance %s does not exist anymore: %s\n%s", "getSecretOptions", err, response)
				}
				return nil, "", err
			}
			failStates := map[string]bool{"destroyed": true}
			if failStates[*stateObj.StateDescription] {
				return stateObj, *stateObj.StateDescription, fmt.Errorf("The instance %s failed: %s\n%s", "getSecretOptions", err, response)
			}
			return stateObj, *stateObj.StateDescription, nil
		},
		Timeout:    d.Timeout(schema.TimeoutCreate),
		Delay:      0 * time.Second,
		MinTimeout: 5 * time.Second,
	}

	return stateConf.WaitForState()
}

func resourceIbmSmKvSecretRead(context context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	secretsManagerClient, err := meta.(conns.ClientSession).SecretsManagerV2()
	if err != nil {
		return diag.FromErr(err)
	}

	id := strings.Split(d.Id(), "/")
	region := id[0]
	instanceId := id[1]
	secretId := id[2]
	secretsManagerClient = getClientWithInstanceEndpoint(secretsManagerClient, instanceId, region, getEndpointType(secretsManagerClient, d))

	getSecretOptions := &secretsmanagerv2.GetSecretOptions{}

	getSecretOptions.SetID(secretId)

	secretIntf, response, err := secretsManagerClient.GetSecretWithContext(context, getSecretOptions)
	if err != nil {
		if response != nil && response.StatusCode == 404 {
			d.SetId("")
			return nil
		}
		log.Printf("[DEBUG] GetSecretWithContext failed %s\n%s", err, response)
		return diag.FromErr(fmt.Errorf("GetSecretWithContext failed %s\n%s", err, response))
	}

	secret := secretIntf.(*secretsmanagerv2.KVSecret)

	if err = d.Set("secret_id", secretId); err != nil {
		return diag.FromErr(fmt.Errorf("Error setting secret_id: %s", err))
	}
	if err = d.Set("instance_id", instanceId); err != nil {
		return diag.FromErr(fmt.Errorf("Error setting instance_id: %s", err))
	}
	if err = d.Set("region", region); err != nil {
		return diag.FromErr(fmt.Errorf("Error setting region: %s", err))
	}
	if err = d.Set("created_by", secret.CreatedBy); err != nil {
		return diag.FromErr(fmt.Errorf("Error setting created_by: %s", err))
	}
	if err = d.Set("created_at", flex.DateTimeToString(secret.CreatedAt)); err != nil {
		return diag.FromErr(fmt.Errorf("Error setting created_at: %s", err))
	}
	if err = d.Set("crn", secret.Crn); err != nil {
		return diag.FromErr(fmt.Errorf("Error setting crn: %s", err))
	}
	if err = d.Set("downloaded", secret.Downloaded); err != nil {
		return diag.FromErr(fmt.Errorf("Error setting downloaded: %s", err))
	}
	if err = d.Set("locks_total", flex.IntValue(secret.LocksTotal)); err != nil {
		return diag.FromErr(fmt.Errorf("Error setting locks_total: %s", err))
	}
	if err = d.Set("name", secret.Name); err != nil {
		return diag.FromErr(fmt.Errorf("Error setting name: %s", err))
	}
	if err = d.Set("secret_group_id", secret.SecretGroupID); err != nil {
		return diag.FromErr(fmt.Errorf("Error setting secret_group_id: %s", err))
	}
	if err = d.Set("secret_type", secret.SecretType); err != nil {
		return diag.FromErr(fmt.Errorf("Error setting secret_type: %s", err))
	}
	if err = d.Set("state", flex.IntValue(secret.State)); err != nil {
		return diag.FromErr(fmt.Errorf("Error setting state: %s", err))
	}
	if err = d.Set("state_description", secret.StateDescription); err != nil {
		return diag.FromErr(fmt.Errorf("Error setting state_description: %s", err))
	}
	if err = d.Set("updated_at", flex.DateTimeToString(secret.UpdatedAt)); err != nil {
		return diag.FromErr(fmt.Errorf("Error setting updated_at: %s", err))
	}
	if err = d.Set("versions_total", flex.IntValue(secret.VersionsTotal)); err != nil {
		return diag.FromErr(fmt.Errorf("Error setting versions_total: %s", err))
	}
	if secret.CustomMetadata != nil {
		d.Set("custom_metadata", secret.CustomMetadata)
	}
	if err = d.Set("description", secret.Description); err != nil {
		return diag.FromErr(fmt.Errorf("Error setting description: %s", err))
	}
	if secret.Labels != nil {
		if err = d.Set("labels", secret.Labels); err != nil {
			return diag.FromErr(fmt.Errorf("Error setting labels: %s", err))
		}
	}
	if secret.Data != nil {
		d.Set("data", secret.Data)
	}

	return nil
}

func resourceIbmSmKvSecretUpdate(context context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	secretsManagerClient, err := meta.(conns.ClientSession).SecretsManagerV2()
	if err != nil {
		return diag.FromErr(err)
	}

	id := strings.Split(d.Id(), "/")
	region := id[0]
	instanceId := id[1]
	secretId := id[2]
	secretsManagerClient = getClientWithInstanceEndpoint(secretsManagerClient, instanceId, region, getEndpointType(secretsManagerClient, d))

	updateSecretMetadataOptions := &secretsmanagerv2.UpdateSecretMetadataOptions{}

	updateSecretMetadataOptions.SetID(secretId)

	hasChange := false

	patchVals := &secretsmanagerv2.SecretMetadataPatch{}

	if d.HasChange("name") {
		patchVals.Name = core.StringPtr(d.Get("name").(string))
		hasChange = true
	}
	if d.HasChange("description") {
		patchVals.Description = core.StringPtr(d.Get("description").(string))
		hasChange = true
	}
	if d.HasChange("labels") {
		labels := d.Get("labels").([]interface{})
		labelsParsed := make([]string, len(labels))
		for i, v := range labels {
			labelsParsed[i] = fmt.Sprint(v)
		}
		patchVals.Labels = labelsParsed
		hasChange = true
	}
	if d.HasChange("custom_metadata") {
		patchVals.CustomMetadata = d.Get("custom_metadata").(map[string]interface{})
		hasChange = true
	}

	if hasChange {
		updateSecretMetadataOptions.SecretMetadataPatch, _ = patchVals.AsPatch()
		_, response, err := secretsManagerClient.UpdateSecretMetadataWithContext(context, updateSecretMetadataOptions)
		if err != nil {
			log.Printf("[DEBUG] UpdateSecretMetadataWithContext failed %s\n%s", err, response)
			return diag.FromErr(fmt.Errorf("UpdateSecretMetadataWithContext failed %s\n%s", err, response))
		}
	}

	return resourceIbmSmKvSecretRead(context, d, meta)
}

func resourceIbmSmKvSecretDelete(context context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	secretsManagerClient, err := meta.(conns.ClientSession).SecretsManagerV2()
	if err != nil {
		return diag.FromErr(err)
	}

	id := strings.Split(d.Id(), "/")
	region := id[0]
	instanceId := id[1]
	secretId := id[2]
	secretsManagerClient = getClientWithInstanceEndpoint(secretsManagerClient, instanceId, region, getEndpointType(secretsManagerClient, d))

	deleteSecretOptions := &secretsmanagerv2.DeleteSecretOptions{}

	deleteSecretOptions.SetID(secretId)

	response, err := secretsManagerClient.DeleteSecretWithContext(context, deleteSecretOptions)
	if err != nil {
		log.Printf("[DEBUG] DeleteSecretWithContext failed %s\n%s", err, response)
		return diag.FromErr(fmt.Errorf("DeleteSecretWithContext failed %s\n%s", err, response))
	}

	d.SetId("")

	return nil
}

func resourceIbmSmKvSecretMapToSecretPrototype(d *schema.ResourceData) (*secretsmanagerv2.KVSecretPrototype, error) {
	model := &secretsmanagerv2.KVSecretPrototype{}
	model.SecretType = core.StringPtr("kv")

	if _, ok := d.GetOk("name"); ok {
		model.Name = core.StringPtr(d.Get("name").(string))
	}
	if _, ok := d.GetOk("description"); ok {
		model.Description = core.StringPtr(d.Get("description").(string))
	}
	if _, ok := d.GetOk("secret_group_id"); ok {
		model.SecretGroupID = core.StringPtr(d.Get("secret_group_id").(string))
	}
	if _, ok := d.GetOk("labels"); ok {
		labels := d.Get("labels").([]interface{})
		labelsParsed := make([]string, len(labels))
		for i, v := range labels {
			labelsParsed[i] = fmt.Sprint(v)
		}
		model.Labels = labelsParsed
	}
	if _, ok := d.GetOk("data"); ok {
		model.Data = d.Get("data").(map[string]interface{})
	}
	if _, ok := d.GetOk("custom_metadata"); ok {
		model.CustomMetadata = d.Get("custom_metadata").(map[string]interface{})
	}
	if _, ok := d.GetOk("version_custom_metadata"); ok {
		model.VersionCustomMetadata = d.Get("version_custom_metadata").(map[string]interface{})
	}
	return model, nil
}
