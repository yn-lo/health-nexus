function deptModalData() {
    var treeEl = document.getElementById('dept-tree-data');
    var selectedEl = document.getElementById('patient-dept-ids-data');
    var publicEl = document.getElementById('public-dept-ids-data');

    var treeData = [];
    var selectedData = [];
    var publicData = [];

    if (treeEl) {
        try { treeData = JSON.parse(treeEl.textContent); } catch (e) {}
    }
    if (selectedEl) {
        try { selectedData = JSON.parse(selectedEl.textContent); } catch (e) {}
    }
    if (publicEl) {
        try { publicData = JSON.parse(publicEl.textContent); } catch (e) {}
    }

    if (!Array.isArray(treeData)) treeData = [];
    if (!Array.isArray(selectedData)) selectedData = [];
    if (!Array.isArray(publicData)) publicData = [];

    var storageKey = 'dept_filter_' + window.location.pathname.replace(/\//g, '_');

    var defaultSelected = selectedData.concat(publicData);

    return {
        tree: treeData,
        selectedDepts: defaultSelected,
        myDeptGroups: [],
        publicDeptGroups: [],

        init() {
            var saved = sessionStorage.getItem(storageKey);
            if (saved) {
                try {
                    var parsed = JSON.parse(saved);
                    if (Array.isArray(parsed)) {
                        this.selectedDepts = parsed;
                    }
                } catch (e) {}
            }
            this._buildDeptGroups();
        },

        _buildDeptGroups() {
            var myGroups = [];
            var publicGroups = [];
            for (var i = 0; i < this.tree.length; i++) {
                var node = this.tree[i];
                if (!node) continue;
                var myLeaves = [];
                var publicLeaves = [];
                this._collectLeafNodesByType(node, myLeaves, publicLeaves);
                if (myLeaves.length > 0) {
                    myGroups.push({ id: node.id, name: node.name, leaves: myLeaves });
                }
                if (publicLeaves.length > 0) {
                    publicGroups.push({ id: node.id, name: node.name, leaves: publicLeaves });
                }
            }
            this.myDeptGroups = myGroups;
            this.publicDeptGroups = publicGroups;
        },

        _collectLeafNodesByType(node, myLeaves, publicLeaves) {
            if (!node) return;
            if (!node.children || node.children.length === 0) {
                var leaf = { id: String(node.id), name: node.name, is_public: !!node.is_public };
                if (node.is_public) {
                    publicLeaves.push(leaf);
                } else {
                    myLeaves.push(leaf);
                }
                return;
            }
            for (var i = 0; i < node.children.length; i++) {
                this._collectLeafNodesByType(node.children[i], myLeaves, publicLeaves);
            }
        },

        isDeptSelected(deptId) {
            return this.selectedDepts.indexOf(String(deptId)) !== -1;
        },

        toggleDept(deptId) {
            var idx = this.selectedDepts.indexOf(String(deptId));
            if (idx === -1) {
                this.selectedDepts.push(String(deptId));
            } else {
                this.selectedDepts.splice(idx, 1);
            }
        },

        saveDepartments() {
            var self = this;
            var metaEl = document.querySelector('meta[name="csrf-token"]');
            var csrfToken = metaEl ? metaEl.getAttribute('content') : '';
            var headers = { 'Content-Type': 'application/x-www-form-urlencoded', 'X-Requested-With': 'XMLHttpRequest' };
            if (csrfToken) headers['X-CSRFToken'] = csrfToken;
            var body = 'departments=' + self.selectedDepts.join('&departments=');
            fetch('/select-departments/', {
                method: 'POST',
                headers: headers,
                body: body
            })
            .then(function() {
                sessionStorage.removeItem(storageKey);
                window.dispatchEvent(new CustomEvent('dept-selector:close'));
                window.dispatchEvent(new CustomEvent('dept-selector:saved', { detail: { departments: self.selectedDepts } }));
            })
            .catch(function(err) {
                if (err && err.name === 'AbortError') {
                    return;
                }
                if (typeof err === 'object' && err.message && (
                    err.message.indexOf('network') !== -1 ||
                    err.message.indexOf('Failed to fetch') !== -1 ||
                    err.message.indexOf('aborted') !== -1
                )) {
                    return;
                }
                window.dispatchEvent(new CustomEvent('dept-selector:close'));
            });
        },

        confirmSelection() {
            sessionStorage.setItem(storageKey, JSON.stringify(this.selectedDepts));
            var deptIds = this.selectedDepts.join(',');
            var url = window.location.pathname + '?dept=' + (deptIds || 'all');
            var searchParams = new URLSearchParams(window.location.search);
            var q = searchParams.get('q');
            if (q) url += '&q=' + encodeURIComponent(q);
            window.location.href = url;
        }
    };
}

function deptModalDataWithOpen(locked) {
    var data = deptModalData();
    data.open = false;
    data.locked = locked !== undefined ? !!locked : false;
    return data;
}
