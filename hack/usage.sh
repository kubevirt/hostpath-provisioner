#!/bin/bash -e

if [ -z $1 ]; then
    echo "node to check not set"
    exit 1
fi

jsonpath="jsonpath={range .items[?(@.metadata.annotations.kubevirt\.io/provisionOnNode==\"$1\")]}{@.metadata.name} {@.spec.resources.requests.storage} {@.status.capacity.storage}{\"\n\"}"
IFS=$'\n'

pvcs=$(kubectl get pvc --all-namespaces -o "$jsonpath")
echo "Examing usage on node $1"
requested=0
total=0

for pvc in $pvcs; do
    IFS=$' '
    x=($pvc)
    echo "PVC: ${x[0]}" 
    if [[ ${x[1]} == *Gi ]]; then
	request=${x[1]::-2}
	requested=$(($requested + $request))
    fi
    if [[ ${x[1]} == *Mi ]]; then
        request=${x[1]::-2}
        requested=$(($requested + $request/1024))
    fi
    if [[ ${x[2]} == *Gi ]]; then
        total=${x[2]::-2}
    fi
done

echo "Total requested: ${requested}Gi"
if [ $total -eq "0" ]; then
    echo "Total capacity: N/A"
else
    echo "Total capacity: ${total}Gi"
fi
