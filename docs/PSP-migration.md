PSP Migration
This doc is currently WIP
 
Related issue: https://github.com/tektoncd/pipeline/issues/4112
 
What is PSP:
 
What is PSA:
 
Why do we have this doc:
 
 
Goal: 
Weight between the 3 options
Migrate from PSP to PAP
PAP is less  granular than PSP – need to figure out where our PSP match up with PAP
Use OPA to enforce policies
Not ideal - adding another dependency
Remove PSP
Users have to enforce their own policies
Migration guide
 
Reference:
PSP:  https://kubernetes.io/docs/concepts/security/pod-security-policy/
PSA: https://kubernetes.io/docs/concepts/security/pod-security-admission/
PSP Spec:
https://pwittrock.github.io/docs/concepts/policy/pod-security-policy/
https://docs.openshift.com/online/pro/rest_api/policy/podsecuritypolicy-policy-v1beta1.html
Open policy agent: https://www.openpolicyagent.org/
 








 
0. Decide whether Pod Security Admission fits for our use case
Default Security Constraints 
	PSA is a non-mutating admission controller.
Check: if with PSP we modify the pods before validation
If yes, do either: 
Modify workloads to meet Pod Security Constraints
Use a mutating admission webhook
101-podsecuirtypolicy.yaml - wise
It does not seem like we are mutating the pod. This check shall pass.
 
Fine-grained Control over Policy Definition
Pod Security Admission only supports 3 standard levels. 
Check: If you require more control over specific constraints,
If yes, do either: 
use a Validating Admission Webhook to enforce those policies.
101-podsecuirtypolicy.yaml - wise
In PSP, both `privileged` and `allowPrivilegeEscalation` are set to `false`, meaning that a pod cannot request to be run as privileged.
Volumes:
For PSP, there seems no support for CSI but only for ISCSI
For PSA, the `restricted` level could cover all the types we currently have plus projected, PVC and csi.
hostNetwork/ hostIPC/ hostPID
All set to `false` in PSP and the `Baseline` level shall suffice. 
runAsUser
For PSP, `MustRunAsNonRoot` rule requires that the pod be submitted with a non-zero runAsUser or have the USER directive defined in the image.
For PSA, the `Restricted` level should suffice
runAsGroup - TODO
seLinux
For PSP, the `seLinux` rule is set to ‘RunAsAny’,  meaning No default provided. Allows any seLinuxOptions to be specified. 
For PSA, this would require the `Privileged` level as at the `Baseline` level, setting the SELinux type is restricted, and setting a custom SELinux user or role option is forbidden. 
This shall require the `privileged` level
supplementalGroups/ fsGroup
Set as a range with `mustRunAs` rule in PSP but not specified in PSA
 
Sub-namespace policy granularity - 
PodSecurityPolicy lets you bind different policies to different Service Accounts or users, even within a single namespace. 
Check: If we bind different policies to different Service Accounts 
If yes, use a 3rd party webhook
Use static configuration for exemptions if you only need to completely exempt specific users or RuntimeClasses.
 
Guide: 
TODO If we go with the first option of using PSA with some other specified policies.
Review namespace permissions
Simplify & standardize PodSecurityPolicies
Update namespaces
Identify an appropriate Pod Security level
Verify the Pod Security level
Enforce the Pod Security level
Bypass PodSecurityPolicy
Review namespace creation processes
Disable PodSecurityPolicy


