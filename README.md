# This is an idrac management utility

idracula generates instackenv.json file suitable for OpenStack installer based on
TripleO like RDO-manager or OSP-directory.

By design, idracula pick the first NIC with a 1GB link as the PXE interface. The hosts must be
running before idracula is started, this because the NIC has to be active during the discovery.
If you use auto-negotation, double check the NIC get the link negotiated as expected.

Once a NIC has be found, idracula will switch its PXE boot parameter on.

Right now, it only scans address ranges and spits out JSON for the dracs it finds.

Example:

    $ ./idracula -u root -p 'password' -scan '192.168.128.1-192.168.128.254'
    {
      "nodes": [
        {
	  "pm_type": "pxe_drac",
      	  "pm_user": "root",
      	  "pm_password": "password",
      	  "pm_addr": "192.168.128.24",
      	  "mac": [
            "d4:ae:52:89:61:ed",
            "d4:ae:52:89:61:ee",
            "d4:ae:52:89:61:ef",
            "d4:ae:52:89:61:f0"
      	  ],
      	  "cpu": "12",
      	  "memory": "98304",
      	  "disk": "7",
      	  "arch": "x86_64"
    	},
    	{
      	  "pm_type": "pxe_drac",
      	  "pm_user": "root",
      	  "pm_password": "password",
      	  "pm_addr": "192.168.128.21",
      	  "mac": [
            "a0:36:9f:52:a2:d8",
            "a0:36:9f:52:a2:da",
            "ec:f4:bb:cd:3b:60",
            "ec:f4:bb:cd:3b:62",
            "ec:f4:bb:cd:3b:64",
            "ec:f4:bb:cd:3b:65"
      	  ],
      	  "cpu": "16",
      	  "memory": "131072",
      	  "disk": "0",
      	  "arch": "x86_64"
    	},
        {
      	  "pm_type": "pxe_drac",
      	  "pm_user": "root",
      	  "pm_password": "password",
      	  "pm_addr": "192.168.128.31",
      	  "mac": [
            "a0:36:9f:53:13:78",
            "a0:36:9f:53:13:7a",
            "ec:f4:bb:cf:ed:98",
            "ec:f4:bb:cf:ed:9a",
            "ec:f4:bb:cf:ed:9c",
            "ec:f4:bb:cf:ed:9d"
      	  ],
      	  "cpu": "20",
      	  "memory": "98304",
      	  "disk": "1",
      	  "arch": "x86_64"
        }
      ]
    }
